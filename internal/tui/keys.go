package tui

import "github.com/charmbracelet/bubbles/key"

type GlobalKeys struct {
	Quit     key.Binding
	Tab      key.Binding
	ShiftTab key.Binding
	Tab1     key.Binding
	Tab2     key.Binding
}

var Keys = GlobalKeys{
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	ShiftTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "cycle agents")),
	Tab1:     key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "tickets")),
	Tab2:     key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "review queue")),
}

type ListKeys struct {
	Up      key.Binding
	Down    key.Binding
	Delete  key.Binding
	Back    key.Binding
	Status  key.Binding
	Thread  key.Binding
	Review  key.Binding
	Stack   key.Binding
	Backlog key.Binding
}

var ListBindings = ListKeys{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Delete:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Status:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "change status")),
	Thread:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "threads")),
	Review:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "review")),
	Stack:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "stack view")),
	Backlog: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "backlog toggle")),
}

type ThreadKeys struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Reply   key.Binding
	Resolve key.Binding
	New     key.Binding
	Back    key.Binding
}

var ThreadBindings = ThreadKeys{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand/collapse")),
	Reply:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
	Resolve: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "resolve")),
	New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new thread")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
