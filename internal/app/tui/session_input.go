package tui

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/MuhametSmaili/hetki/internal/directory"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

const (
	sessionNameField      = "name"
	sessionDirectoryField = "directory"
)

func (a *LiveAdapter) sessionPrompt(itemID list.ItemID) (ui.ActionResult, error) {
	a.mu.RLock()
	input, item := a.input, a.index[itemID]
	a.mu.RUnlock()

	path := ""
	if input != nil {
		if destination := prepareNavigation(input.state.StateResult, item); destination != nil {
			path = destination.path
		}
	}

	if path == "" {
		var err error
		path, err = os.UserHomeDir()
		if err != nil {
			return ui.ActionResult{}, fmt.Errorf("find home directory: %w", err)
		}
	}

	return ui.ActionResult{Input: &ui.InputPrompt{
		Title:        "CREATE SESSION",
		SubmitLabel:  "create",
		SubmitStatus: "creating session...",
		Fields: []ui.InputField{
			{
				ID:          sessionNameField,
				Label:       "Session name",
				Kind:        ui.InputText,
				Placeholder: "Type session name…",
				Required:    true,
			},
			{
				ID:           sessionDirectoryField,
				Label:        "Directory",
				Kind:         ui.InputDirectory,
				InitialValue: path,
				BasePath:     path,
				Required:     true,
			},
		},
	}}, nil
}

func validateSessionInput(request ui.ActionRequest) (string, string, error) {
	name := strings.TrimSpace(request.Values[sessionNameField])
	if name == "" {
		return "", "", fmt.Errorf("session name is required")
	}
	if strings.ContainsAny(name, ".:") || strings.ContainsFunc(name, unicode.IsControl) {
		return "", "", fmt.Errorf("session name cannot contain dots, colons or control characters")
	}
	path, err := directory.Validate(request.Values[sessionDirectoryField], "")
	if err != nil {
		return "", "", err
	}
	return name, path, nil
}
