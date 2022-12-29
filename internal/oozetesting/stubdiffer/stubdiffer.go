package stubdiffer

type StubDiffer struct {
	diff string
}

func New(diff string) *StubDiffer {
	return &StubDiffer{
		diff: diff,
	}
}

func (d *StubDiffer) Diff(_, _ string, _, _ []byte) string {
	return d.diff
}
