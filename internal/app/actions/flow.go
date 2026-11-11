package actions

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
)

// Screen values are immutable until the next flow operation. Keys identify UI
// history only; accepting a choice always checks the currently published items.
type Screen struct {
	Key       string
	Title     string
	Items     []Item
	Done      bool
	EmptyText string
}

// Flow has a single serialized owner. All operations are synchronous and honor
// the caller's context; only the application schedules them away from rendering.
type Flow struct {
	catalog   Catalog
	inventory Inventory
	server    *observe.Server
	request   Request
	recipeID  string
	step      int
	screen    Screen
	items     map[string]Item
	closed    bool
	completed bool
}

func New(ctx context.Context, catalog Catalog, inventory Inventory, subject Subject) (*Flow, error) {
	if err := catalog.validate(); err != nil {
		return nil, err
	}
	if subject != (Subject{}) {
		if err := subject.Validate(); err != nil {
			return nil, err
		}
	}

	snapshot, err := inventory.QueryActionState(ctx)
	if err != nil {
		return nil, err
	}
	resolved, err := initialSubject(subject, snapshot.State)
	if err != nil {
		return nil, err
	}
	request := Request{Version: 1, Context: Context{Subject: resolved}, Steps: make(map[string]Item)}
	if snapshot.Socket != "" {
		request.Context.Server = &ServerContext{Socket: snapshot.Socket}
	}
	if invocation := snapshot.State.Invocation; invocation != nil {
		request.Context.Invocation = &Invocation{SessionID: invocation.SessionID, PaneID: invocation.PaneID}
	}
	f := &Flow{catalog: catalog, inventory: inventory, server: snapshot.Server, request: request}
	f.menu()
	return f, nil
}

func (f *Flow) Screen() Screen { return f.screen }

// Select accepts only an item from the current screen. Commands execute once
// per explicit selection; failure leaves a separate run screen for deliberate retry.
func (f *Flow) Select(ctx context.Context, id string) (Screen, error) {
	if err := ctx.Err(); err != nil {
		return f.screen, err
	}
	item, ok := f.items[id]
	if !ok || f.closed {
		return f.screen, fmt.Errorf("selected action item is stale")
	}
	if f.completed {
		return f.finish(), nil
	}
	if f.recipeID == "" {
		f.recipeID, f.step = id, 0
		f.request.Steps = make(map[string]Item)
		return f.advance(ctx)
	}
	step := f.currentStep()
	if step.Run != nil {
		return f.execute(ctx, *step.Run)
	}
	f.request.Steps[step.Select] = item
	f.step++
	return f.advance(ctx)
}

func (f *Flow) advance(ctx context.Context) (Screen, error) {
	screen, err := f.enter(ctx)
	if err != nil || f.currentStep().Run == nil {
		return screen, err
	}
	return f.execute(ctx, *f.currentStep().Run)
}

func (f *Flow) Back(ctx context.Context) (Screen, error) {
	if f.closed {
		return f.screen, fmt.Errorf("action flow is closed")
	}
	if f.completed {
		return f.finish(), nil
	}
	if f.recipeID == "" {
		return f.finish(), nil
	}
	if f.step == 0 {
		f.menu()
		return f.screen, nil
	}
	f.step--
	delete(f.request.Steps, f.currentStep().Select)
	return f.enter(ctx)
}

// Refresh reloads only candidates. A run screen is never executed by refresh.
func (f *Flow) Refresh(ctx context.Context) (Screen, error) {
	if f.closed {
		return f.screen, fmt.Errorf("action flow is closed")
	}
	if f.completed {
		return f.screen, nil
	}
	if f.recipeID == "" {
		f.menu()
		return f.screen, nil
	}
	return f.enter(ctx)
}

func (f *Flow) menu() {
	f.recipeID, f.step = "", 0
	f.request.Steps = make(map[string]Item)
	var items []Item
	kind, title := "", "Actions"
	emptyText := "no global actions configured"
	if subject := f.request.Context.Subject; subject != nil {
		kind, title = subject.Kind, title+" · "+subject.Kind
		emptyText = "no actions for this " + kind
	}
	for _, id := range slices.Sorted(maps.Keys(f.catalog.Actions)) {
		recipe := f.catalog.Actions[id]
		if slices.Contains(recipe.Scope, "global") || slices.Contains(recipe.Scope, kind) {
			items = append(items, Item{ID: id, Label: recipe.Label})
		}
	}
	f.publish(Screen{Key: "actions", Title: title, Items: items, EmptyText: emptyText})
}

func (f *Flow) currentStep() Step { return f.catalog.Actions[f.recipeID].Steps[f.step] }

func (f *Flow) enter(ctx context.Context) (Screen, error) {
	step := f.currentStep()
	title := step.Title
	if title == "" {
		title = step.Select
	}
	screen := Screen{Key: f.recipeID + "/" + strconv.Itoa(f.step), Title: f.catalog.Actions[f.recipeID].Label + " › " + title}
	if step.Run != nil {
		screen.Title = f.catalog.Actions[f.recipeID].Label
		screen.Items = []Item{{ID: "run", Label: "Run " + screen.Title}}
		f.publish(screen)
		return screen, nil
	}
	// Publish an empty new screen even on failure. Old choices must never be
	// accepted as candidates for a step whose provider failed.
	f.publish(screen)
	items, err := f.load(ctx, *step.Source)
	if err != nil {
		return f.screen, err
	}
	screen.Items = items
	f.publish(screen)
	return screen, nil
}

func (f *Flow) load(ctx context.Context, spec SourceSpec) ([]Item, error) {
	if spec.Items != nil {
		return spec.Items, ctx.Err()
	}
	var source Source = executableSource{}
	if spec.Builtin != "" {
		state, err := f.query(ctx)
		if err != nil {
			return nil, err
		}
		source = inventorySource{sessions: state.Sessions}
	}
	return source.List(ctx, spec, f.request)
}

func (f *Flow) query(ctx context.Context) (backend.StateResult, error) {
	if f.server == nil {
		return backend.StateResult{}, fmt.Errorf("this action source requires a running tmux server; none was available at startup")
	}
	snapshot, err := f.inventory.QueryActionState(ctx)
	if err != nil {
		return backend.StateResult{}, err
	}
	if snapshot.Server == nil || snapshot.Server.Identity() != f.server.Identity() || snapshot.Socket != f.server.Socket {
		return backend.StateResult{}, observe.ErrServerChanged
	}
	return snapshot.State, nil
}

func (f *Flow) execute(ctx context.Context, command Command) (Screen, error) {
	if err := f.revalidate(ctx); err != nil {
		return f.screen, err
	}
	result, err := runCommand(ctx, command, f.request)
	if err != nil {
		if result.exitedSuccessfully {
			f.completed = true
			f.publish(Screen{
				Key: f.recipeID + "/done", Title: "Completed · " + f.catalog.Actions[f.recipeID].Label,
				Items: []Item{{ID: "done", Label: "Close completed action", Description: err.Error()}},
			})
		}
		return f.screen, err
	}
	return f.finish(), nil
}

func (f *Flow) revalidate(ctx context.Context) error {
	if f.server == nil {
		return ctx.Err()
	}
	state, err := f.query(ctx)
	if err != nil {
		return err
	}
	return f.validateTargets(state.Sessions)
}

func (f *Flow) finish() Screen {
	f.closed = true
	f.screen = Screen{Done: true}
	return f.screen
}

func (f *Flow) validateTargets(sessions []backend.Session) error {
	if subject := f.request.Context.Subject; subject != nil {
		if _, err := resolveSubject(*subject, sessions); err != nil {
			return err
		}
	}
	for name, item := range f.request.Steps {
		if item.target == nil {
			continue
		}
		target := item.target
		kind := "session"
		if target.PaneID != "" {
			kind = "pane"
		} else if target.WindowID != "" {
			kind = "window"
		}
		subject := Subject{Kind: kind, SessionID: target.SessionID, WindowID: target.WindowID, PaneID: target.PaneID}
		if _, err := resolveSubject(subject, sessions); err != nil {
			return fmt.Errorf("selection %q: %w", name, err)
		}
	}
	return nil
}

func (f *Flow) publish(screen Screen) {
	f.screen = screen
	f.items = make(map[string]Item, len(screen.Items))
	for _, item := range screen.Items {
		f.items[item.ID] = item
	}
}
