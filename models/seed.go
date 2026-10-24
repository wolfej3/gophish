package models

// Insert helpers write a single row directly. They exist so that tests in
// other packages (and seeding scripts) can set up campaign activity without
// the full send pipeline.

// InsertCampaign inserts a campaign row directly.
func InsertCampaign(c *Campaign) error {
	return db.Create(c).Error
}

// InsertResult inserts a result row directly.
func InsertResult(r *Result) error {
	return db.Create(r).Error
}

// InsertEvent inserts an event row directly.
func InsertEvent(e *Event) error {
	return db.Create(e).Error
}
