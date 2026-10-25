package tui

import ui "github.com/MuhametSmaili/hetki/internal/tui"

// Overrides contains only explicitly supplied invocation choices.
// Nil fields leave the corresponding file preference or default unchanged.
type Overrides struct {
	Height *ui.Height
	Border *ui.BorderMode
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
	return nil
}

func (o Overrides) apply(options ui.Options) ui.Options {
	if o.Height != nil {
		options.Height = *o.Height
	}
	if o.Border != nil {
		options.Border = *o.Border
	}
	return options
}
