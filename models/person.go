package models

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Person aggregates a recipient's activity across every campaign in a team,
// keyed by email address. Counts are the number of distinct campaigns in
// which the action happened, so repeated events in one campaign count once.
type Person struct {
	Email     string     `json:"email"`
	FirstName string     `json:"first_name"`
	LastName  string     `json:"last_name"`
	Campaigns int64      `json:"campaigns"`
	Opened    int64      `json:"opened"`
	Clicked   int64      `json:"clicked"`
	Submitted int64      `json:"submitted"`
	Reported  int64      `json:"reported"`
	LastEvent *time.Time `json:"last_event"`
}

// PersonCampaign is one campaign a person took part in, with the furthest
// state they reached in it.
type PersonCampaign struct {
	CampaignId   int64     `json:"campaign_id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Reported     bool      `json:"reported"`
	SendDate     time.Time `json:"send_date"`
	ModifiedDate time.Time `json:"modified_date"`
}

// PersonEvent is a single event in a person's cross-campaign timeline.
type PersonEvent struct {
	CampaignId int64     `json:"campaign_id"`
	Name       string    `json:"campaign_name"`
	Message    string    `json:"message"`
	Time       time.Time `json:"time"`
	Details    string    `json:"details"`
}

// PersonDetail is a person's aggregate plus their per-campaign history and
// event timeline.
type PersonDetail struct {
	Person
	History  []PersonCampaign `json:"history"`
	Timeline []PersonEvent    `json:"timeline"`
}

// personResultRow is the per-email aggregate from the results table.
type personResultRow struct {
	Email        string
	FirstName    string
	LastName     string
	Campaigns    int64
	Reported     int64
	LastModified scanTime
}

// personEventRow is a per-email, per-message count from the events table.
type personEventRow struct {
	Email    string
	Message  string
	Count    int64
	LastTime scanTime
}

// GetPeople returns every recipient who has appeared in a campaign in the
// team, with their cross-campaign activity.
func GetPeople(teamID int64) ([]Person, error) {
	// Base counts come from results: one result row exists per campaign a
	// person was targeted in, and reported is a flag on it.
	resultRows := []personResultRow{}
	err := db.Table("results").
		Select("email, MAX(first_name) as first_name, MAX(last_name) as last_name, "+
			"COUNT(DISTINCT campaign_id) as campaigns, "+
			"SUM(CASE WHEN reported THEN 1 ELSE 0 END) as reported, "+
			"MAX(modified_date) as last_modified").
		Where("team_id = ?", teamID).
		Group("email").
		Scan(&resultRows).Error
	if err != nil {
		return nil, err
	}

	people := make(map[string]*Person, len(resultRows))
	order := make([]string, 0, len(resultRows))
	for _, r := range resultRows {
		last := r.LastModified.Time
		p := &Person{
			Email:     r.Email,
			FirstName: r.FirstName,
			LastName:  r.LastName,
			Campaigns: r.Campaigns,
			Reported:  r.Reported,
		}
		if !last.IsZero() {
			t := last
			p.LastEvent = &t
		}
		people[r.Email] = p
		order = append(order, r.Email)
	}

	// Opened/clicked/submitted come from events, which log each action, so a
	// person who submitted data is also counted as having opened and clicked.
	eventRows := []personEventRow{}
	err = db.Table("events").
		Select("events.email, events.message, COUNT(DISTINCT events.campaign_id) as count, MAX(events.time) as last_time").
		Joins("JOIN campaigns ON campaigns.id = events.campaign_id").
		Where("campaigns.team_id = ? AND events.message IN (?)", teamID,
			[]string{EventOpened, EventClicked, EventDataSubmit}).
		Group("events.email, events.message").
		Scan(&eventRows).Error
	if err != nil {
		return nil, err
	}
	for _, e := range eventRows {
		p, ok := people[e.Email]
		if !ok {
			continue
		}
		switch e.Message {
		case EventOpened:
			p.Opened = e.Count
		case EventClicked:
			p.Clicked = e.Count
		case EventDataSubmit:
			p.Submitted = e.Count
		}
		if e.LastTime.Time.After(deref(p.LastEvent)) {
			t := e.LastTime.Time
			p.LastEvent = &t
		}
	}

	people2 := make([]Person, 0, len(order))
	for _, email := range order {
		people2 = append(people2, *people[email])
	}
	return people2, nil
}

// GetPerson returns one recipient's aggregate, per-campaign history, and
// event timeline within the team. If the email has no results in the team,
// the returned detail has zero campaigns.
func GetPerson(teamID int64, email string) (PersonDetail, error) {
	detail := PersonDetail{Person: Person{Email: email}}

	history := []PersonCampaign{}
	err := db.Table("results").
		Select("results.campaign_id, campaigns.name, results.status, results.reported, results.send_date, results.modified_date").
		Joins("JOIN campaigns ON campaigns.id = results.campaign_id").
		Where("results.team_id = ? AND results.email = ?", teamID, email).
		Order("results.send_date DESC").
		Scan(&history).Error
	if err != nil {
		return detail, err
	}
	detail.History = history

	timeline := []PersonEvent{}
	err = db.Table("events").
		Select("events.campaign_id, campaigns.name, events.message, events.time, events.details").
		Joins("JOIN campaigns ON campaigns.id = events.campaign_id").
		Where("campaigns.team_id = ? AND events.email = ?", teamID, email).
		Order("events.time DESC").
		Scan(&timeline).Error
	if err != nil {
		return detail, err
	}
	detail.Timeline = timeline

	// Derive the aggregate from the history and timeline so it matches what's
	// shown, without another round of queries.
	detail.Campaigns = int64(len(history))
	openedIn, clickedIn, submittedIn := map[int64]bool{}, map[int64]bool{}, map[int64]bool{}
	for _, c := range history {
		if c.Reported {
			detail.Reported++
		}
	}
	for _, e := range timeline {
		switch e.Message {
		case EventOpened:
			openedIn[e.CampaignId] = true
		case EventClicked:
			clickedIn[e.CampaignId] = true
		case EventDataSubmit:
			submittedIn[e.CampaignId] = true
		}
		if detail.LastEvent == nil || e.Time.After(*detail.LastEvent) {
			t := e.Time
			detail.LastEvent = &t
		}
	}
	detail.Opened = int64(len(openedIn))
	detail.Clicked = int64(len(clickedIn))
	detail.Submitted = int64(len(submittedIn))

	// Names come from the most recent result row.
	if len(history) > 0 {
		latest := Result{}
		err = db.Table("results").
			Where("team_id = ? AND email = ?", teamID, email).
			Order("modified_date DESC").
			Take(&latest).Error
		if err == nil {
			detail.FirstName = latest.FirstName
			detail.LastName = latest.LastName
			if detail.LastEvent == nil && !latest.ModifiedDate.IsZero() {
				t := latest.ModifiedDate
				detail.LastEvent = &t
			}
		}
	}
	return detail, nil
}

// scanTime scans a datetime column into a time.Time, accepting both the
// native time.Time that the MySQL driver returns and the string that SQLite
// returns for aggregate expressions like MAX(modified_date).
type scanTime struct {
	time.Time
}

// Value implements driver.Valuer so gorm treats scanTime as a scannable
// field. scanTime is only ever read, so this isn't used in practice.
func (s scanTime) Value() (driver.Value, error) {
	if s.Time.IsZero() {
		return nil, nil
	}
	return s.Time, nil
}

func (s *scanTime) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		s.Time = time.Time{}
		return nil
	case time.Time:
		s.Time = v
		return nil
	case []byte:
		return s.parse(string(v))
	case string:
		return s.parse(v)
	default:
		return fmt.Errorf("cannot scan %T into a time", value)
	}
}

func (s *scanTime) parse(value string) error {
	if value == "" {
		s.Time = time.Time{}
		return nil
	}
	layouts := []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05-07:00",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			s.Time = t.UTC()
			return nil
		}
	}
	return fmt.Errorf("cannot parse time %q", value)
}

// deref returns the time a pointer holds, or the zero time if it's nil.
func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
