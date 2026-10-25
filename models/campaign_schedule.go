package models

import (
	"errors"
	"fmt"
	"time"

	log "github.com/gophish/gophish/logger"
)

// Repeat intervals supported by a CampaignSchedule.
const (
	IntervalOnce    string = "once"
	IntervalDaily   string = "daily"
	IntervalWeekly  string = "weekly"
	IntervalMonthly string = "monthly"
)

// CampaignSchedule is a recurring (or one-off, future-dated) recipe that spawns
// a new Campaign each time it comes due. Unlike a Campaign, it references its
// template, landing page, sending profile, and groups by name, so each run
// resolves them fresh — picking up current group membership (e.g. from
// directory sync) at launch time.
type CampaignSchedule struct {
	Id     int64 `json:"id"`
	UserId int64 `json:"-"`
	// TeamId is the team the schedule and its spawned campaigns belong to.
	TeamId         int64           `json:"team_id" gorm:"column:team_id"`
	Name           string          `json:"name" gorm:"not null"`
	TemplateName   string          `json:"template_name"`
	PageName       string          `json:"page_name"`
	SMTPName       string          `json:"smtp_name"`
	URL            string          `json:"url"`
	RepeatInterval string          `json:"repeat_interval"`
	SendByOffset   int64           `json:"send_by_offset"` // minutes over which to drip each run; 0 = all at once
	NextLaunchDate time.Time       `json:"next_launch_date"`
	LastLaunchDate time.Time       `json:"last_launch_date"`
	Enabled        bool            `json:"enabled"`
	CreatedDate    time.Time       `json:"created_date"`
	Groups         []ScheduleGroup `json:"groups" gorm:"foreignkey:ScheduleId;constraint:OnDelete:CASCADE"`
}

// ScheduleGroup records one group (by name) that a CampaignSchedule targets.
type ScheduleGroup struct {
	Id         int64  `json:"-"`
	ScheduleId int64  `json:"-"`
	GroupName  string `json:"name" gorm:"column:group_name"`
}

// TableName overrides the gorm-inferred table name.
func (ScheduleGroup) TableName() string {
	return "schedule_groups"
}

// ErrScheduleNameNotSpecified indicates no name was given for the schedule.
var ErrScheduleNameNotSpecified = errors.New("Schedule name not specified")

// ErrInvalidInterval indicates an unsupported repeat interval was given.
var ErrInvalidInterval = errors.New("Invalid repeat interval")

// ErrScheduleNotFound indicates the schedule does not exist.
var ErrScheduleNotFound = errors.New("Schedule not found")

// validIntervals is the set of accepted repeat intervals.
var validIntervals = map[string]bool{
	IntervalOnce:    true,
	IntervalDaily:   true,
	IntervalWeekly:  true,
	IntervalMonthly: true,
}

// Validate checks the schedule for missing or invalid fields.
func (s *CampaignSchedule) Validate() error {
	switch {
	case s.Name == "":
		return ErrScheduleNameNotSpecified
	case len(s.Groups) == 0:
		return ErrGroupNotSpecified
	case s.TemplateName == "":
		return ErrTemplateNotSpecified
	case s.PageName == "":
		return ErrPageNotSpecified
	case s.SMTPName == "":
		return ErrSMTPNotSpecified
	case !validIntervals[s.RepeatInterval]:
		return ErrInvalidInterval
	}
	return nil
}

// advanceFrom returns the next launch time after t for this schedule's
// interval, or the zero time for a one-off schedule (which does not repeat).
func (s *CampaignSchedule) advanceFrom(t time.Time) time.Time {
	switch s.RepeatInterval {
	case IntervalDaily:
		return t.AddDate(0, 0, 1)
	case IntervalWeekly:
		return t.AddDate(0, 0, 7)
	case IntervalMonthly:
		return t.AddDate(0, 1, 0)
	default: // IntervalOnce
		return time.Time{}
	}
}

// BuildCampaign constructs a Campaign ready to be passed to PostCampaign,
// launching immediately (LaunchDate = launchTime). The template, page, SMTP,
// and groups are referenced by name so PostCampaign resolves them fresh.
func (s *CampaignSchedule) BuildCampaign(launchTime time.Time) Campaign {
	c := Campaign{
		Name:       fmt.Sprintf("%s - %s", s.Name, launchTime.Format("2006-01-02 15:04")),
		LaunchDate: launchTime,
		URL:        s.URL,
		Template:   Template{Name: s.TemplateName},
		Page:       Page{Name: s.PageName},
		SMTP:       SMTP{Name: s.SMTPName},
	}
	if s.SendByOffset > 0 {
		c.SendByDate = launchTime.Add(time.Duration(s.SendByOffset) * time.Minute)
	}
	for _, g := range s.Groups {
		c.Groups = append(c.Groups, Group{Name: g.GroupName})
	}
	return c
}

// GetCampaignSchedules returns all schedules belonging to the given team.
func GetCampaignSchedules(tid int64) ([]CampaignSchedule, error) {
	ss := []CampaignSchedule{}
	err := db.Where("team_id=?", tid).Preload("Groups").Find(&ss).Error
	if err != nil {
		log.Error(err)
	}
	return ss, err
}

// GetCampaignSchedule returns the schedule with the given id, scoped to the team.
func GetCampaignSchedule(id int64, tid int64) (CampaignSchedule, error) {
	s := CampaignSchedule{}
	err := db.Where("id=? AND team_id=?", id, tid).Preload("Groups").First(&s).Error
	return s, err
}

// GetDueSchedules returns the enabled schedules whose next launch time has
// arrived (on or before t), across all teams, for the background worker.
func GetDueSchedules(t time.Time) ([]CampaignSchedule, error) {
	ss := []CampaignSchedule{}
	err := db.Where("enabled=? AND next_launch_date <= ?", true, t.UTC()).
		Preload("Groups").Find(&ss).Error
	if err != nil {
		log.Error(err)
	}
	return ss, err
}

// PostCampaignSchedule validates and inserts a new schedule.
func PostCampaignSchedule(s *CampaignSchedule, uid, tid int64) error {
	if err := s.Validate(); err != nil {
		return err
	}
	s.UserId = uid
	s.TeamId = tid
	s.CreatedDate = time.Now().UTC()
	if s.NextLaunchDate.IsZero() {
		s.NextLaunchDate = s.CreatedDate
	} else {
		s.NextLaunchDate = s.NextLaunchDate.UTC()
	}
	err := db.Save(s).Error
	if err != nil {
		log.Error(err)
	}
	return err
}

// PutCampaignSchedule validates and updates an existing schedule, replacing its
// group list. It is scoped to the team to prevent cross-team edits.
func PutCampaignSchedule(s *CampaignSchedule, tid int64) error {
	if err := s.Validate(); err != nil {
		return err
	}
	existing, err := GetCampaignSchedule(s.Id, tid)
	if err != nil {
		return ErrScheduleNotFound
	}
	s.TeamId = existing.TeamId
	s.UserId = existing.UserId
	s.CreatedDate = existing.CreatedDate
	s.NextLaunchDate = s.NextLaunchDate.UTC()
	// Replace the group associations rather than appending to them.
	if err := db.Where("schedule_id=?", s.Id).Delete(&ScheduleGroup{}).Error; err != nil {
		log.Error(err)
		return err
	}
	err = db.Save(s).Error
	if err != nil {
		log.Error(err)
	}
	return err
}

// AdvanceSchedule records a launch at t: it moves NextLaunchDate forward by the
// interval (disabling the schedule if it was a one-off) and stamps
// LastLaunchDate.
func (s *CampaignSchedule) AdvanceSchedule(t time.Time) error {
	next := s.advanceFrom(t.UTC())
	updates := map[string]interface{}{
		"last_launch_date": t.UTC(),
		"next_launch_date": next,
	}
	if next.IsZero() {
		updates["enabled"] = false
	}
	return db.Model(&CampaignSchedule{}).Where("id=?", s.Id).Updates(updates).Error
}

// DeleteCampaignSchedule removes a schedule and its group associations, scoped
// to the team.
func DeleteCampaignSchedule(id int64, tid int64) error {
	s, err := GetCampaignSchedule(id, tid)
	if err != nil {
		return ErrScheduleNotFound
	}
	if err := db.Where("schedule_id=?", s.Id).Delete(&ScheduleGroup{}).Error; err != nil {
		log.Error(err)
		return err
	}
	return db.Where("id=?", s.Id).Delete(&CampaignSchedule{}).Error
}
