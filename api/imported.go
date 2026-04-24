package thunderstormmock

type status int

const (
	waiting status = iota
	inProgress
	finished
	crashed
)

func (s status) String() string {
	switch s {
	case waiting:
		return "Waiting for execution"
	case inProgress:
		return "Currently being scanned"
	case finished:
		return "Sample analysis complete"
	case crashed:
		return "Sample analysis failed"
	default:
		return "invalid"
	}
}
