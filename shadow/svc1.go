package shadow

type Svc1 struct {
	name string
}

func (s *Svc1) SetName(name string) {
	s.name = name
}

func (s *Svc1) GetName() string {
	return s.name
}

func (s *Svc1) String() string {
	return "Svc1"
}
