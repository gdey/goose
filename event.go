package goose

type Eventer interface {
	event()
	IsEqual(e Eventer) bool
}

type Event struct{}

func (*Event) event() {}

func AreEventsEqual(e1, e2 Eventer) bool {
	if e1 == nil {
		return e2 == nil
	}
	if e2 == nil {
		return false
	}
	return e1.IsEqual(e2)
}
