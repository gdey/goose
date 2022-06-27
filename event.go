package goose

type Eventer interface {
	event()
}

type Event struct{}

func (*Event) event() {}
