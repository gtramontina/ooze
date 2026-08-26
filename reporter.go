package ooze

// Reporter publishes the terminal result of one campaign.
type Reporter interface {
	Report(Result) error
}
