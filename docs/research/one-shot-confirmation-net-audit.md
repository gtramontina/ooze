# One-shot confirmation net: adversarial decision audit

_Research date: 2026-08-24. Independent audit input for [Repair the one-shot confirmation
net](https://github.com/gtramontina/ooze/issues/74). This is not an implementation specification._

## Verdict

The current behavior is internally coherent, implemented as resolved, and more accurately described
as **retiring an overlap-attribution mechanism after the runtime removes Ooze-owned overlap** than as
destroying a general timeout-safety net. Confirmation was narrowed to one purpose: resolving a
deadline from an obligation with recorded Ooze peer overlap. `SingleAdmissionAutomatic` makes that
predicate false for future primaries, so direct `TimedOut` classification follows rather than evades
the confirmation rule.

On a strict reading of the ticket's fixed input, process-lifetime retention **is the only permitted
policy**. [Repair the one-shot confirmation net](https://github.com/gtramontina/ooze/issues/74)
says to preserve [Define the campaign transition algebra](https://github.com/gtramontina/ooze/issues/57)
and may amend [Set automatic admission and pressure fallback](https://github.com/gtramontina/ooze/issues/58),
but the transition-algebra refinement rule 5 independently fixes the complete
broker state and its irreversible, idempotent transition. Campaign-scoping therefore amends the
transition algebra as
well as the pressure-fallback decision. The candidate list is explicitly “not decisions” and cannot override the fixed-input
clause. This is an internal inconsistency in the ticket, resolved by its own priority wording: retain the
process-wide one-way policy, publish the missing rationale, and correct the overbroad “last
confirmation” premise.

If the owner intended “preserve the transition algebra except clauses copied from the pressure-fallback
decision,” that exception is not written
and must not be inferred silently under the map's non-silent-amendment rule. With an explicit new
authorization to amend the transition algebra, campaign-lifetime reset would become a reader-owned policy choice. It
would trade renewed confirmation coverage in later campaigns for repeated exposure to already-
observed pressure. Without that authorization, no reader choice remains.

The other advertised directions are also not admissible under this ticket's fixed input. Confirming
future no-overlap deadlines would amend the timeout-attribution rule and transition algebra, not
merely the pressure-fallback decision. Removing ordinary confirmation as a pressure signal would
amend the timeout-attribution and pressure-fallback decisions unless
[Distinguish an inner command timeout from capacity pressure](https://github.com/gtramontina/ooze/issues/75)
first supplies reliable evidence that an observed
nonzero exit was not the “regular test failure” those decisions name.

## What the closed decisions actually say

- [Confirm suspicious attempts before scoring them](https://github.com/gtramontina/ooze/issues/51)
  makes recorded peer overlap the deterministic attribution boundary. A primary
  deadline without it is immediately authoritative after drainage, including in `Serial()`, single
  admission, and an automatic tail; unrelated external processes explicitly do not count. A deadline
  with it receives at most one fresh exclusive confirmation. Pass maps to `Survived`, regular failure
  to `Killed`, and a repeated deadline to `TimedOut`; ordinary confirmation also validates pressure
  ([resolution refinement](https://github.com/gtramontina/ooze/issues/51#issuecomment-5366364348)).
- [Define the campaign transition algebra](https://github.com/gtramontina/ooze/issues/57) repeats the
  same narrowed state-machine rule and command-count consequence: only an
  overlap-provisional mutant is confirmed, exactly once in a completed campaign. Its rule 5 also
  independently fixes the complete broker state and calls the transition irreversible and idempotent
  ([refinement rules 1–5](https://github.com/gtramontina/ooze/issues/57#issuecomment-5366364784)).
- [Set automatic admission and pressure fallback](https://github.com/gtramontina/ooze/issues/58)
  makes `FullAutomatic(P) | SingleAdmissionAutomatic` the complete automatic state, permits
  exactly two pressure paths, and makes the transition one-way and process-runtime-wide until a fresh
  Go process. Single admission changes future automatic grants, not the `GOMAXPROCS=1` attempt profile
  ([resolution](https://github.com/gtramontina/ooze/issues/58#issuecomment-5366365199)).
- [Calibrate baseline-derived mutation deadlines](https://github.com/gtramontina/ooze/issues/59)
  resolves the deadline once from baseline duration and permitted peers. Its `1.5` peer term
  includes a judgmental 2x external-contention allowance precisely because external load cannot
  create recorded overlap; its `5` term covers legitimate mutant work and one-sample baseline error
  ([resolution](https://github.com/gtramontina/ooze/issues/59#issuecomment-5378843338)). Tightening a
  deadline raises the reported score and loosening lowers it
  ([codicil](https://github.com/gtramontina/ooze/issues/59#issuecomment-5380593455)).
- The destination says Ooze “confirms only overlap-ambiguous suspicious attempts before scoring,”
  prefers replayable transitions, and permits closed decisions to be amended only explicitly
  ([map](https://github.com/gtramontina/ooze/issues/48)).
- Scoring is `detected / total`, where `detected = Killed + TimedOut + Runaway`; therefore only a
  confirmation that passes changes the numerator relative to direct `TimedOut`. Failure versus
  deadline changes the diagnostic bucket but not the numeric score
  ([Define user-visible outcome and scoring semantics](https://github.com/gtramontina/ooze/issues/63#issuecomment-5378068036)).

These clauses make the important distinction: a deadline can be “false” relative to an imagined
unloaded execution, yet still be attributable under the resolved observation model. Confirmation is
not a general retry for sampling error or external contention. Recasting it that way would reopen the
closed overlap boundary.

## Current code and test evidence

The implementation at `2bd4a97` follows the decisions without an accidental extra one-shot switch:

- `newProcessRuntime` begins in `fullAutomatic`; the state has only `fullAutomatic` and
  `singleAdmission` ([source](../../internal/ooze/process_runtime.go#L131-L156),
  [constructor](../../internal/ooze/process_runtime.go#L292-L298)).
- Accepted start commitments latch overlap symmetrically only against other committed live
  obligations ([source](../../internal/ooze/process_runtime.go#L404-L431));
  tests prove that uncommitted work is excluded and overlap remains latched after peer settlement
  ([test](../../internal/ooze/process_runtime_contract_internal_test.go#L171-L203)).
- Only an overlapped primary deadline becomes provisional and installs a barrier
  ([source](../../internal/ooze/process_runtime.go#L556-L576)).
- Hard shared launch exhaustion and accepted ordinary confirmation each transition the runtime only
  from full to single; there is no reverse assignment
  ([hard pressure](../../internal/ooze/process_runtime.go#L517-L541),
  [confirmation pressure](../../internal/ooze/process_runtime.go#L651-L680)).
- Granting uses the captured capacity in full mode and limit one in single mode
  ([source](../../internal/ooze/process_runtime.go#L822-L855)).
- Demotion on one confirmation does **not** discard provisionals already in the same sealed queue.
  Later queued confirmations continue while the campaign gate stays closed, and only the last reopens
  it ([test](../../internal/ooze/process_runtime_contract_internal_test.go#L231-L268)).
  Thus “the first successful confirmation is also the last” is only true for future primaries, not
  for provisionals already captured in that closure wave.
- Hard-exhaustion demotion likewise does not revoke active shared work. Already-live obligations may
  retain recorded overlap, later trip, and earn confirmation after the runtime mode has changed. The
  one-shot description applies only once the pre-transition shared set and already-earned queue have
  drained.

## Behavioral choices and consequences

| Choice | Contract impact | Numeric-score consequence | Correctness / operational consequence |
| --- | --- | --- | --- |
| Keep process-wide one-way demotion | No closed decision changes. Add rationale to this ticket and the map only. | Future no-overlap deadlines are direct detected `TimedOut`. Compared with a hypothetical confirmation, only would-pass confirmations could lower the score; ordinary failure or repeat deadline remains detected. | Coherent with overlap-only confirmation. Remembers pressure across campaigns and avoids repeatedly provoking it. External contention and deadline sampling remain deliberately handled only by the calibrated deadline's margins. |
| Reset after the pressure-owning campaign set terminates | Explicitly amend the pressure-fallback decision **and transition-algebra rule 5**, plus map/domain wording. As this ticket is written, this violates fixed input. The broker must track all campaigns that contribute pressure before resetting, not merely the first one. | Current demoted campaign is unchanged. Later campaigns can again form overlap and confirm; a pass changes `TimedOut` to `Survived` and lowers the score, while failure/repeated deadline only changes provenance/bucket or nothing. | Deterministic if campaign ownership and terminal events are trace state. Restores later-campaign confirmation, but deliberately re-exposes every later campaign to known pressure; hard exhaustion may repeatedly abort campaigns unscored and ordinary-pressure cases repeatedly pay exclusive confirmation. |
| Give bounded confirmations to later single-admission primaries | Amend the timeout-attribution rule and the transition algebra's confirmation/command-count invariants, contrary to this ticket's fixed input. A budget also needs a scope and reset policy. | Each added confirmation can turn a detected direct timeout into `Survived`; otherwise the numerator is unchanged. | Converts confirmation into a general deadline retry. Budget exhaustion makes catalogue/campaign position affect which otherwise identical no-overlap deadline gets rescue evidence. Not an available repair under this ticket's fixed input. |
| Stop treating ordinary confirmation termination as pressure | Amend the timeout-attribution rule, transition-algebra rule 5, and pressure-fallback decision as written, unless the inner-timeout decision introduces a trustworthy distinct observation that is not a regular failure. | Keeps full admission, so later overlap deadlines continue to receive confirmation; would-pass cases may lower subsequent scores. The triggering confirmation's score is unchanged because outcome and feedback are separate. | An inner timeout weakens outcome diagnostics but does not erase the pressure observation: the primary hit Ooze's outer deadline with peers, while the exclusive confirmation terminated before that same outer deadline. Removing that signal discards the currently resolved comparison. Repeated confirmation cost and pressure continue. |

“Campaign-scoped mode” cannot safely mean only “limit the triggering campaign to one.” Other
campaigns share the same process broker and can still overlap it, so that form neither removes the
suspected process-local pressure nor guarantees no recorded peer overlap. The coherent scoped form is
a **global** single-admission epoch owned by a nonempty set of pressure-reporting campaigns, with a
reset only after that set terminates. If a second campaign reports pressure while already single due
to pre-transition committed work, dropping its ownership and resetting when the first ends would
erase evidence.

## Adversarial checks

1. **Is this actually one-shot?** Only for future primary-generated provisionals. Already queued
   provisionals survive the transition and are confirmed. The live ticket should not claim otherwise.
2. **Does single admission prove an unloaded host?** No. It proves only absence of simultaneous
   Ooze-owned committed peers. The timeout-attribution decision intentionally excludes CI co-tenants
   and other Ooze processes.
3. **Does that make direct timeout logically invalid?** Not under the accepted attribution model.
   It exposes a residual real-world false-positive risk, acknowledged and priced into the calibrated
   deadline, but the
   same risk already exists for `Serial()` and automatic tail attempts.
4. **Would a process-wide reset be free?** No. The observed pressure may be a stable property of this
   process/host/command. Resetting converts remembered evidence into recurring confirmation cost or
   recurring hard-exhaustion aborts.
5. **Does an inner timeout negate pressure evidence?** No. It can make authoritative classification
   `Killed` instead of the more diagnostic `TimedOut`, but the admission comparison still holds: the
   shared primary reached Ooze's outer deadline, whereas the exclusive confirmation ended before the
   same outer deadline. In a `go test` command, overlap may delay compilation or the start of the test
   binary's own timeout clock; reaching that inner timeout only in the exclusive attempt can itself
   be evidence that removing peers changed whether the command fit within Ooze's bound. Like an
   ordinary pass/failure, it is correlation under the accepted observation model, not proof against
   all fluctuating external causes. The inner-timeout ticket's premise therefore does not force
   narrowing the pressure-fallback decision; that ticket remains
   open and offers unresolved candidates
   ([Distinguish an inner command timeout from capacity pressure](https://github.com/gtramontina/ooze/issues/75)).

## Publication implication

The fixed inputs force a unique outcome: process-wide one-way retention. Publish the rationale,
correct the “last confirmation” overstatement, and leave implementation code unchanged. If the owner
wants campaign-lifetime reset instead, this ticket must first be changed to authorize an explicit
transition-algebra amendment as well as a pressure-fallback amendment; only then does a reader-owned
scheduling choice exist. Any implementation
would remain a separate delivery issue.
