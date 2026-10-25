package tui

import ui "github.com/MuhametSmaili/hetki/internal/tui"

// Overrides contains only explicitly supplied invocation choices.
// Nil fields leave the corresponding file preference or default unchanged.
type Overrides struct {
	Height       *ui.Height
	Border       *ui.BorderMode
	Preview      *bool
	Scope        *ui.Scope
	View         *ui.ViewMode
	PreviewWidth *int
}

func (o Overrides) Validate() error {
	if o.Height != nil {
		if err := o.Height.Validate(); err != nil {
			return err
		}
	}
	if o.Border != nil {
		if err := o.Border.Validate(); err != nil {
			return err
		}
	}
	if o.Scope != nil {
		if err := o.Scope.Validate(); err != nil {
			return err
		}
	}
	if o.View != nil {
		if err := o.View.Validate(); err != nil {
			return err
		}
	}
	if o.PreviewWidth != nil {
		return ui.ValidatePreviewWidth(*o.PreviewWidth)
	}
	return nil
}

func (o Overrides) apply(options ui.Options) ui.Options {
	if o.Height != nil {
		options.Height = *o.Height
	}
	if o.Border != nil {
		options.Border = *o.Border
	}
	if o.Preview != nil {
		options.Initial.Preview = *o.Preview
	}
	if o.Scope != nil {
		options.Initial.Scope = *o.Scope
	}
	if o.View != nil {
		options.Initial.View = *o.View
	}
	if o.PreviewWidth != nil {
		options.PreviewWidth = *o.PreviewWidth
	}
	return options
}
