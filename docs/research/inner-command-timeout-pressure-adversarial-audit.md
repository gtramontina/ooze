# Inner command timeout versus capacity pressure: adversarial audit

_Audit date: 2026-08-24. Scope: live issues
[Wayfinder: Make mutation execution self-managing](https://github.com/gtramontina/ooze/issues/48),
[Distinguish an inner command timeout from capacity pressure](https://github.com/gtramontina/ooze/issues/75),
[Confirm suspicious attempts before scoring them](https://github.com/gtramontina/ooze/issues/51),
[Define the campaign transition algebra](https://github.com/gtramontina/ooze/issues/57),
[Set automatic admission and pressure fallback](https://github.com/gtramontina/ooze/issues/58),
[Calibrate baseline-derived mutation deadlines](https://github.com/gtramontina/ooze/issues/59),
[Define the supervised attempt contract](https://github.com/gtramontina/ooze/issues/61),
[Add managed admission, process-local coordination, and vNext API](https://github.com/gtramontina/ooze/issues/70), and
[Repair the one-shot confirmation net](https://github.com/gtramontina/ooze/issues/74), plus the current
`feat/managed-mutation-execution` source and history. This note is an independent
challenge, not an implementation specification._

## Executive finding

The issue combines two different questions. The evidence strongly favors one
answer, but the accepted decisions deliberately leave owner policy judgment to
the inner-timeout decision.

1. **Pressure feedback is already justified.** If an overlapped primary reaches
   Ooze's command deadline, while its fresh exclusive confirmation exits nonzero
   because of an inner command timeout _by that same Ooze deadline_, the
   primary deadline has disappeared under exclusivity. That is exactly the accepted
   definition of `Capacity pressure`; the inner reason for the confirmation's
   nonzero exit does not erase the observed command-level differential.
2. **A generic inner-timeout diagnostic is not observable.** An opaque command can
   encode an internal timeout with any exit status or output, and ordinary command
   output can imitate a known timeout string. Ooze has enough retained evidence to
   attempt a heuristic, but no evidence that makes the heuristic authoritative.

Recommendation: keep the pressure-fallback admission transition unchanged and
keep a nonzero root exit with no Ooze bound as ordinary `Settled`/killed-class
evidence. If the owner accepts that policy, close the inner-timeout decision as a
rejected diagnostic heuristic. The owner selected that recommendation: preserve
generic `Killed` and existing pressure feedback for every drained pass or nonzero
`Settled` confirmation. Add no output recognizer or public hook, and do not narrow
validation to passing confirmations.

## The false-pressure premise does not survive a concrete timeline

Let `D` be the one mutation deadline resolved after the baseline. The
deadline-calibration decision fixes one value for the primary and confirmation; it
is not recomputed for exclusivity
([resolution](https://github.com/gtramontina/ooze/issues/59#issuecomment-5378843338)).
The suspicious-attempt decision requires the confirmation to be fresh,
process-local exclusive, and to retain that full deadline
([refinement](https://github.com/gtramontina/ooze/issues/51#issuecomment-5366364348)).

The disputed trace therefore has only two relevant command-level observations:

```text
shared primary:        no root exit by D -> Ooze deadline
exclusive confirmation: root exits nonzero at t <= D -> ordinary settlement
```

The `t <= D` relation follows from the accepted tie rule. Root exit wins when it
is already observable at exact deadline equality; otherwise Ooze's same command
deadline selects a second deadline trip. The confirmation's inner timeout may
explain _why_ the root exited nonzero; it cannot change the fact that the whole
opaque command settled by `D` only on the exclusive observation.

This is precisely the glossary definition: capacity pressure includes "a primary
deadline with recorded peer overlap that disappears under exclusive confirmation"
([`CONTEXT.md`](https://github.com/gtramontina/ooze/blob/2bd4a97/CONTEXT.md#L67-L69)).
It is also the pressure-fallback decision's second accepted pressure path: an
overlapped primary deadline followed by an exclusive ordinary pass or regular
failure changes future admission, while classification remains separate
([resolution](https://github.com/gtramontina/ooze/issues/58#issuecomment-5366365199)).

Here, "ordinary" is necessarily boundary language: the supervised-attempt
contract's `Settled` means that the root exited and no Ooze command bound selected
terminal intent. It does not claim that Ooze can distinguish a failed assertion
from every command-internal reason for a nonzero exit. Reading "regular test
failure" more narrowly would require evidence the opaque command contract does not
supply.

The adversarial counter-cases reinforce rather than weaken that conclusion:

- If the inner timeout also ends the shared primary before `D`, there is no primary
  Ooze deadline, no confirmation, and no pressure transition.
- If the inner timeout ends neither observation before `D`, both attempts reach the
  Ooze deadline and the pressure-fallback decision makes no admission change.
- External load or test nondeterminism can also produce a shared/exclusive
  differential. That limitation belongs to the already-accepted one-confirmation
  attribution model; an inner timeout does not introduce a distinct epistemic
  defect.

Calling the disputed transition "false pressure" would silently redefine capacity
pressure to mean that exclusivity rescued the mutant from every intrinsic failure.
The accepted model defines it more narrowly and operationally: exclusivity removed
the Ooze deadline. Admission feedback changes future overlap; it does not determine
the mutant outcome.

## Why the diagnostic cannot be authoritative across an opaque boundary

The pressure-fallback decision explicitly says Ooze does not parse or rewrite
`WithTestCommand`, infer command weights, or claim a resource quota over its subtree
([resolution](https://github.com/gtramontina/ooze/issues/58#issuecomment-5366365199)).
The public option accepts arbitrary commands, including make targets
([`options.go`](../../options.go#L38-L46)); only the default happens to be
`go test -count=1 ./...` ([`release.go`](../../release.go#L49-L52)).

The supervised-attempt contract intentionally preserves the evidence seam without
assigning semantics to it: the supervisor retains fired bound, deadline, command
duration, root exit, and an immutable merged-output prefix, while a nonzero exit
with no Ooze bound remains ordinary `Settled`
([resolution](https://github.com/gtramontina/ooze/issues/61#issuecomment-5386039287)).
The current implementation matches that contract:

- `ExecutionData` exposes the resolved deadline, command duration, fired bound, and
  output snapshot, including completeness/finality
  ([`supervisor.go`](../../internal/ooze/supervisor.go#L253-L279));
- `Settled` carries normalized exit status separately from `Tripped`
  ([`supervisor.go`](../../internal/ooze/supervisor.go#L291-L335));
- the reducer contract test explicitly pins that a nonzero root exit stays
  settled pending the inner-timeout decision
  ([`supervisor_terminal_normalization_reducer_internal_test.go`](../../internal/ooze/supervisor_terminal_normalization_reducer_internal_test.go#L180-L190)).

That evidence supports a heuristic, not a proof. Even a final, complete output
snapshot only proves which bytes the command wrote. A test can print the same
`panic: test timed out` text before an unrelated failure; a wrapper can transform
or omit it; a non-Go command can express an inner timeout differently. Partial or
failed capture weakens the negative case further. Command duration also cannot
identify the cause of a nonzero exit.

The implementation history confirms this boundary was deliberate rather than an
accidental omission: signed contract commit
[`14c532d`](https://github.com/gtramontina/ooze/commit/14c532d389b6cbff05af4a93dfb434c67f8196b5)
reserved classification for the inner-timeout decision; later commits
[`b83db73`](https://github.com/gtramontina/ooze/commit/b83db73) and
[`9b0fd1d`](https://github.com/gtramontina/ooze/commit/9b0fd1d) retained immutable
output and normalized terminal evidence without interpreting command text.

## Authority and ordering

The live [Add managed admission, process-local coordination, and vNext
API](https://github.com/gtramontina/ooze/issues/70) does not close this decision
frontier.

1. The pressure-fallback decision published the pressure rule on 2026-08-21: a
   confirmation which "terminates ordinarily, by pass or regular test failure"
   validates pressure.
2. The inner-timeout decision was opened on 2026-08-22 as a `wayfinder:grilling`
   decision specifically because that rule does not say whether an inner timeout
   is a regular failure or false pressure evidence.
3. The supervised-attempt decision published its contract on 2026-08-23, after
   both. It retained nonzero/no-Ooze-bound as structural `Settled` evidence, stated
   that the inner-timeout decision alone may interpret the output/duration
   signature for killed-class diagnostics or overlap-pressure validation, and
   recorded that it re-read and updated the fixed inputs of both the
   managed-admission delivery and inner-timeout decisions.
4. The managed-admission delivery is labelled `managed-execution:delivery`. Its
   acceptance restates the pressure-fallback decision as "terminates ordinarily"
   and its later supervised-attempt amendment allocates final production
   integration to that delivery; neither text claims authority to define
   "ordinarily" for an opaque command or retracts the supervised-attempt decision's
   delegation to the inner-timeout decision.

The binding order is therefore the pressure-fallback policy, followed by the
supervised-attempt contract's explicit semantic seam and allocation to the
inner-timeout decision, with the managed-admission delivery as a downstream
consumer. If the inner-timeout decision keeps the existing rule, the delivery
needs no change. If it excludes recognized inner timeouts, that is an explicit
amendment to the pressure-fallback decision whose one-line consequence must be
propagated before delivery. The delivery's current mirror of the pressure-fallback
decision is evidence of the present default, not a higher-authority decision that
prevents the allocated decision ticket from amending it.

## Decision tree

### Admission feedback

| Option | Evidence consequence | Assessment |
| --- | --- | --- |
| Keep pass-or-nonzero ordinary confirmation | Uses the shared/exclusive command-bound differential already accepted by the suspicious-attempt and pressure-fallback decisions | **Recommended; no policy change** |
| Demote only after a passing confirmation | Avoids the alleged case but discards genuine pressure evidence whenever exclusivity lets a mutant reach an ordinary failing assertion | Reopens the pressure-fallback decision and silently adopts a reader-owned policy |
| Exclude recognized timeout output | Makes admission depend on spoofable, tool-specific text even though the command-bound differential still exists | Unsound and contrary to the opaque boundary |

### Mutant diagnostic

| Option | Evidence consequence | Assessment |
| --- | --- | --- |
| Keep nonzero/no-Ooze-bound as `Killed` | Claims only what the opaque process observation supports; score is unchanged | **Recommended** |
| Privately recognize a known `go test` signature | Keep `Killed`, attach an honest "command reported an inner timeout" diagnostic, and optionally withhold pressure; false positives/negatives are unavoidable | Viable alternative requiring owner judgment and explicit amendments to the suspicious-attempt, campaign-algebra, and pressure-fallback decisions |
| Privately relabel the result `TimedOut` | Improves a common diagnostic but blurs the accepted meaning that Ooze's command deadline fired | Reject |
| Add a caller classification protocol/hook | Could make tool-specific semantics explicit | Potentially sound later, but speculative public surface forbidden by the campaign-algebra and pressure-fallback decisions and the project map |

## Independent adversarial check

An independent primary-evidence pass agreed that the opaque boundary prevents
universal inner-timeout classification, but recommended the private recognizer:
only for final, complete, nonzero `Settled` output with no Ooze bound and an
anchored Go timeout-panic signature; retain `Killed`, emit a non-authoritative
diagnostic, and withhold pressure acceptance. Its argument is asymmetric risk: a
false positive suppresses a process-local performance fallback rather than
changing a score, while a false negative preserves today's demotion.

That is the strongest alternative to the recommendation above. It still cannot
prove provenance, and withholding pressure discards the command-bound differential
demonstrated by the concrete timeline. It therefore needs an explicit policy
amendment; it is not a correction already forced by the suspicious-attempt,
campaign-algebra, and pressure-fallback decisions.

## Closure test

Accepted decisions did **not** force closure. The suspicious-attempt,
campaign-algebra, and pressure-fallback decisions literally include regular
confirmation failure in pressure validation, while the supervised-attempt contract
deliberately left the inner-timeout decision authority to interpret retained output
and decide whether it changes killed-class diagnostics or overlap-pressure
validation. The owner chose:

1. preserve authoritative opaque-command semantics and the existing pressure
   transition; and
2. reject a private, best-effort Go signature as sufficient to annotate the
   diagnostic or withhold pressure.

The evidence rules out presenting the rejected recognizer as authoritative or
generic. The decision also rejects narrowing pressure to passing confirmations,
because that would discard all genuine ordinary-failure differentials and broadly
reopen the suspicious-attempt, campaign-algebra, and pressure-fallback decisions.

This answer is compatible with [Repair the one-shot confirmation
net](https://github.com/gtramontina/ooze/issues/74): pressure feedback remains
one-way and process-runtime-wide, already-earned confirmation queues still drain,
and the inner-timeout decision does not erase the shared/exclusive timing comparison
([resolution](https://github.com/gtramontina/ooze/issues/74#issuecomment-5395499464)).
