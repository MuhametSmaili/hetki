package statusobserver

import (
	"context"
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status/profile"
	"github.com/MuhametSmaili/hetki/internal/status/service"
)

func loadProfiles(ctx context.Context) (*profile.Catalog, error) {
	dir, err := profile.DefaultDirectory()
	if err != nil {
		return nil, fmt.Errorf("locate status profiles: %w", err)
	}
	return profile.LoadDirectory(ctx, dir)
}

func profileSelector(catalog *profile.Catalog) func(observe.Pane) Selection {
	return func(pane observe.Pane) Selection {
		p, found := catalog.Match(pane.Command)
		if !found {
			return Selection{}
		}
		selected := Selection{Profile: service.Profile{Source: service.Protocol, Generation: p.Generation}}
		if p.Source == profile.Screen {
			selected.Profile.Source = service.Screen
			selected.Evaluate = p.Screen.Evaluate
			selected.PollInterval = p.PollInterval
			// Last-nonblank evaluation needs the full bounded visible viewport.
			selected.Region = observe.Region{}
		}
		return selected
	}
}
