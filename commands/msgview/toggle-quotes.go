package msgview

import (
	"git.sr.ht/~rjarry/aerc/app"
	"git.sr.ht/~rjarry/aerc/commands"
)

type ToggleQuotes struct{}

func init() {
	commands.Register(ToggleQuotes{})
}

func (ToggleQuotes) Description() string {
	return "Toggle the visibility of quoted text in messages."
}

func (ToggleQuotes) Context() commands.CommandContext {
	return commands.MESSAGE_VIEWER
}

func (ToggleQuotes) Aliases() []string {
	return []string{"toggle-quotes"}
}

func (ToggleQuotes) Execute(args []string) error {
	mv, _ := app.SelectedTabContent().(*app.MessageViewer)
	mv.ToggleQuotes()
	return nil
}
