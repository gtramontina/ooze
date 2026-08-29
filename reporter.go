package ooze

// Reporter publishes the terminal result of one campaign.
type Reporter interface {
	Report(Result) error
}

type reportLogger interface {
	Logf(string, ...any)
}

type consoleReporter struct {
	logger reportLogger
}

func (reporter consoleReporter) Report(result Result) error {
	reporter.logger.Logf("%s", result.report.Text)

	return nil
}
