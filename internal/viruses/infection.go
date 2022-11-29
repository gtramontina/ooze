package viruses

type (
	Infect    func()
	Disinfect func()
)

type Infection struct {
	infect    Infect
	disinfect Disinfect
}

func NewInfection(infect Infect, disinfect Disinfect) *Infection {
	return &Infection{
		infect:    infect,
		disinfect: disinfect,
	}
}

func (i *Infection) Mutate(fn func()) {
	defer i.disinfect()
	i.infect()
	fn()
}
