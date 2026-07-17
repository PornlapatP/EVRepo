// Package campaign owns the registration campaign window — the single source
// of truth for whether the form is currently open. It serves the public
// GET /api/v1/campaign (so the frontend can gate its UX), the back-office
// get/update, and the status check the registration submit gate calls
// (registration/ReService) so no one can submit outside the window.
package campaign

import (
	"errors"
	"time"

	"github.com/pornlapatP/EV/internal/models"
	"gorm.io/gorm"
)

// ErrStartNotBeforeEnd is returned by Update when the proposed window is empty
// or inverted (startAt must be strictly before endAt).
var ErrStartNotBeforeEnd = errors.New("campaign startAt must be before endAt")

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// active returns the single active campaign, or (nil, nil) when none is
// configured. If more than one row is flagged active (shouldn't happen — the
// system runs one at a time), the most recently updated wins.
func (s *Service) active() (*models.Campaign, error) {
	var c models.Campaign
	err := s.db.Where("is_active = ?", true).Order("updated_at desc").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Status returns the active campaign (nil if none) and its gate status at now.
// No active campaign fails closed — status is CampaignClosed — so a missing
// configuration blocks registration rather than leaving it wide open
// (campaign-window-plan.md §"ไม่มี campaign active = ปิด").
func (s *Service) Status(now time.Time) (*models.Campaign, models.CampaignStatus, error) {
	c, err := s.active()
	if err != nil {
		return nil, "", err
	}
	if c == nil {
		return nil, models.CampaignClosed, nil
	}
	return c, c.Status(now), nil
}

// Update mutates the active campaign's window/name. If no campaign exists yet
// one is created (so the very first back-office save bootstraps the window
// without needing a seed row). Times are expected in UTC already (the
// back-office converts Thai-local input before sending).
func (s *Service) Update(name string, startAt, endAt time.Time) (*models.Campaign, error) {
	if !startAt.Before(endAt) {
		return nil, ErrStartNotBeforeEnd
	}

	c, err := s.active()
	if err != nil {
		return nil, err
	}
	if c == nil {
		c = &models.Campaign{IsActive: true}
	}
	c.Name = name
	c.StartAt = startAt.UTC()
	c.EndAt = endAt.UTC()
	c.IsActive = true

	if err := s.db.Save(c).Error; err != nil {
		return nil, err
	}
	return c, nil
}
