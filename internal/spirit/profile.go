// Package spirit provides the "精灵" (Spirit) companion system.
//
// A Spirit is a persistent, personalised agent companion that:
//   - Maintains a user's long-term preferences and work context
//   - Routes user commands to appropriate agents
//   - Orchestrates multi-agent collaboration
//   - Learns from feedback to improve over time
package spirit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Profile holds the Spirit's understanding of its user.
type Profile struct {
	ID              string            `json:"id"`
	UserID          string            `json:"user_id"`
	TenantID        string            `json:"tenant_id"`
	Name            string            `json:"name"`             // user's chosen name for their spirit
	Language        string            `json:"language"`          // preferred language
	Style           CommunicationStyle `json:"style"`
	Preferences     Preferences       `json:"preferences"`
	AgentAffinity   map[string]float64 `json:"agent_affinity"`  // agentID → success rate
	WorkContext     WorkContext       `json:"work_context"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	InteractionCount int              `json:"interaction_count"`
}

// CommunicationStyle captures how the spirit communicates.
type CommunicationStyle struct {
	Verbosity   string `json:"verbosity"`    // "concise", "balanced", "detailed"
	Tone        string `json:"tone"`         // "professional", "casual", "technical"
	ProactiveLevel string `json:"proactive"` // "minimal", "moderate", "aggressive"
}

// Preferences captures user's tool and workflow preferences.
type Preferences struct {
	FavoriteTools    []string `json:"favorite_tools"`
	AvoidTools       []string `json:"avoid_tools"`
	DefaultProvider  string   `json:"default_provider"`
	DefaultModel     string   `json:"default_model"`
	WorkHours        string   `json:"work_hours,omitempty"`     // e.g. "09:00-18:00"
	Timezone         string   `json:"timezone,omitempty"`
	AutoApproveTools []string `json:"auto_approve_tools,omitempty"`
}

// WorkContext holds the user's current work state across sessions.
type WorkContext struct {
	CurrentProject string            `json:"current_project,omitempty"`
	RecentTopics   []string          `json:"recent_topics,omitempty"`
	PinnedNotes    []string          `json:"pinned_notes,omitempty"`
	ActiveGoals    []Goal            `json:"active_goals,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	LastActiveAt   time.Time         `json:"last_active_at"`
}

// Goal is a user-defined objective the spirit tracks.
type Goal struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Status      string    `json:"status"` // "active", "completed", "paused"
	Progress    float64   `json:"progress"` // 0.0–1.0
	CreatedAt   time.Time `json:"created_at"`
}

// ProfileStore persists spirit profiles.
type ProfileStore interface {
	GetProfile(ctx context.Context, userID, tenantID string) (*Profile, error)
	SaveProfile(ctx context.Context, profile *Profile) error
	DeleteProfile(ctx context.Context, userID, tenantID string) error
}

// ProfileManager manages spirit profiles with caching.
type ProfileManager struct {
	mu    sync.RWMutex
	cache map[string]*Profile // userID:tenantID → profile
	store ProfileStore
}

// NewProfileManager creates a profile manager.
func NewProfileManager(store ProfileStore) *ProfileManager {
	return &ProfileManager{
		cache: make(map[string]*Profile),
		store: store,
	}
}

// Get returns the profile for a user, creating a default if none exists.
func (pm *ProfileManager) Get(ctx context.Context, userID, tenantID string) (*Profile, error) {
	key := userID + ":" + tenantID

	pm.mu.RLock()
	if p, ok := pm.cache[key]; ok {
		pm.mu.RUnlock()
		return p, nil
	}
	pm.mu.RUnlock()

	// Load from store.
	p, err := pm.store.GetProfile(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	if p == nil {
		p = newDefaultProfile(userID, tenantID)
	}

	pm.mu.Lock()
	pm.cache[key] = p
	pm.mu.Unlock()

	return p, nil
}

// Save persists a profile.
func (pm *ProfileManager) Save(ctx context.Context, profile *Profile) error {
	profile.UpdatedAt = time.Now()

	if err := pm.store.SaveProfile(ctx, profile); err != nil {
		return err
	}

	key := profile.UserID + ":" + profile.TenantID
	pm.mu.Lock()
	pm.cache[key] = profile
	pm.mu.Unlock()

	return nil
}

// RecordInteraction updates the profile with a new interaction.
func (pm *ProfileManager) RecordInteraction(ctx context.Context, userID, tenantID, agentID string, success bool) error {
	p, err := pm.Get(ctx, userID, tenantID)
	if err != nil {
		return err
	}

	p.InteractionCount++
	p.WorkContext.LastActiveAt = time.Now()

	if p.AgentAffinity == nil {
		p.AgentAffinity = make(map[string]float64)
	}

	current := p.AgentAffinity[agentID]
	if success {
		p.AgentAffinity[agentID] = current*0.9 + 0.1
	} else {
		p.AgentAffinity[agentID] = current * 0.95
	}

	return pm.Save(ctx, p)
}

func newDefaultProfile(userID, tenantID string) *Profile {
	now := time.Now()
	return &Profile{
		ID:       fmt.Sprintf("spirit-%s-%d", userID, now.UnixNano()),
		UserID:   userID,
		TenantID: tenantID,
		Name:     "Spirit",
		Language: "en",
		Style: CommunicationStyle{
			Verbosity:      "balanced",
			Tone:           "professional",
			ProactiveLevel: "moderate",
		},
		Preferences:   Preferences{},
		AgentAffinity: make(map[string]float64),
		WorkContext: WorkContext{
			LastActiveAt: now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}
