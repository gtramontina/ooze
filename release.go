package ooze

import (
	"os"
	"testing"

	"github.com/fatih/color"
	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/fstemporarydir"
	"github.com/gtramontina/ooze/internal/gotextdiff"
	"github.com/gtramontina/ooze/internal/iologger"
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/prettydiff"
	"github.com/gtramontina/ooze/internal/scorecalculator"
	"github.com/gtramontina/ooze/internal/testingtlaboratory"
	"github.com/gtramontina/ooze/internal/testingtreporter"
	"github.com/gtramontina/ooze/internal/verboselaboratory"
	"github.com/gtramontina/ooze/internal/verbosereporter"
	"github.com/gtramontina/ooze/internal/verboserepository"
	"github.com/gtramontina/ooze/internal/verbosetemporarydir"
	"github.com/gtramontina/ooze/internal/verbosetestrunner"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
)

func Release(t *testing.T) {
	t.Helper()

	var (
		log          ooze.Logger                   = iologger.New(os.Stdout)
		repository   ooze.Repository               = fsrepository.New(".")
		temporaryDir laboratory.TemporaryDirectory = fstemporarydir.New("ooze-")
		testRunner   laboratory.TestRunner         = cmdtestrunner.New("go", "test", "-count=1", "./...")
		reporter     ooze.Reporter                 = testingtreporter.New(
			t,
			log,
			prettydiff.New(gotextdiff.New()),
			scorecalculator.New(),
			0.5, //nolint:gomnd
		)
	)

	if testing.Verbose() {
		repository = verboserepository.New(t, repository)
		temporaryDir = verbosetemporarydir.New(t, temporaryDir)
		testRunner = verbosetestrunner.New(t, testRunner)
		reporter = verbosereporter.New(t, reporter)
	}

	var lab ooze.Laboratory = laboratory.New(testRunner, temporaryDir)
	if testing.Verbose() {
		lab = verboselaboratory.New(t, lab)
	}

	t.Cleanup(func() {
		t.Helper()
		reporter.Summarize()
	})

	lab = testingtlaboratory.New(t, lab)

	border := color.YellowString
	text := color.CyanString

	log.Logf(border("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓"))
	log.Logf("%[1]s%[2]s%[1]s", border("┃"), text("┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐"))
	log.Logf("%[1]s%[2]s%[1]s", border("┃"), text("│      │  │      │  ┌──────┘  ├─────  "))
	log.Logf("%[1]s%[2]s%[1]s", border("┃"), text("└──────┘  └──────┘  └──────┘  └──────┘"))
	log.Logf(border("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛"))
	log.Logf("Running…")

	ooze.New(repository, lab, reporter).Release(
		integerincrement.New(),
	)
}
