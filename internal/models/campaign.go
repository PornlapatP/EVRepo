package models

import "time"

// CampaignStatus is the server-computed gate state of the registration
// campaign window. The client never computes this from its own clock — every
// endpoint hands back a status derived from the server clock (campaign-window-plan.md).
type CampaignStatus string

const (
	CampaignBefore CampaignStatus = "before" // now < StartAt — not open yet
	CampaignOpen   CampaignStatus = "open"   // StartAt <= now <= EndAt — accepting submissions
	CampaignClosed CampaignStatus = "closed" // now > EndAt — window has ended (also used when no active campaign exists)
)

// Campaign is the single time window during which the EV-Voluntary-Registration-Form
// form may be filled in and submitted. The system runs one active campaign at a
// time (IsActive); staff set its start/end through the back-office. Times are
// stored in UTC per the project's date rule — the back-office renders/edits them
// in Thai local time.
type Campaign struct {
	ID       uint      `gorm:"primaryKey;autoIncrement"`
	Name     string    `gorm:"type:varchar(200);not null"`
	StartAt  time.Time `gorm:"not null"`      // timestamptz, stored UTC
	EndAt    time.Time `gorm:"not null"`      // timestamptz, stored UTC
	IsActive bool      `gorm:"not null;index"` // pick the active row (most recently updated wins if >1)

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Status reports where now sits relative to this campaign's window. Boundaries
// are inclusive of open (StartAt <= now <= EndAt) so a campaign is "open" for
// the whole of its start and end instants.
func (c *Campaign) Status(now time.Time) CampaignStatus {
	switch {
	case now.Before(c.StartAt):
		return CampaignBefore
	case now.After(c.EndAt):
		return CampaignClosed
	default:
		return CampaignOpen
	}
}
