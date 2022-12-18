package viruses

type (
	Infect    func()
	Disinfect func()
)

type Infection struct {
	name      string
	infect    Infect
	disinfect Disinfect
}

func NewInfection(name string, infect Infect, disinfect Disinfect) *Infection {
	return &Infection{
		name:      name,
		infect:    infect,
		disinfect: disinfect,
	}
}

func (i *Infection) Mutate(fn func()) {
	defer i.disinfect()
	i.infect()
	fn()
}

func (i *Infection) String() string {
	return i.name
}
