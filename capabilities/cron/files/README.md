# internal/cron

The scheduler of this project. `weld add cron` installs an in-process scheduler
library; it does **not** schedule any work. Nothing in the generated application
imports this package, so building, testing and every short command (`help`,
`version`, …) run with no scheduler and no jobs. Jobs are registered by the
application at run time, and they run only while a long-running command (`serve`)
has called `Start`.

## You own the lifecycle

```go
sched := cron.New(logger, cron.WithLocation(location))
if err := sched.Register("nightly-report", "0 3 * * *", func(ctx context.Context) {
	report(ctx)
}); err != nil {
	return err
}
sched.Start(ctx)
defer func() {
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = sched.Stop(stopCtx)
}()
```

The scheduler is constructed and started by the process, never by `weld add`.
`Start` is explicit, so help, version and every short command are unaffected.

## What the scheduler guarantees

- **Five-field specs.** `minute hour day-of-month month day-of-week`. The
  six-field seconds form and the `@descriptor` shortcuts are rejected, so a typo
  fails at `Register`, not silently at 3am.
- **One timezone.** Schedules are interpreted in the timezone given to
  `WithLocation`; the default is UTC, so a schedule means the same thing on every
  host. The value comes from `config.Cron` (`cron.timezone`, env
  `CRON_TIMEZONE`, default `UTC`).
- **Unique names.** `Register` refuses a duplicate name (`ErrJobExists`); change
  an existing job with `Reschedule`.
- **Dynamic registry.** `Register`, `Remove` and `Reschedule` are safe before or
  after `Start` and safe for concurrent use.
- **Overlap is skipped.** If a job is still running when its next tick arrives,
  that tick is skipped and logged, never queued.
- **Panics are contained.** A panic inside a job is recovered and logged; the
  process keeps running and the job is not wedged for future ticks.
- **Graceful stop.** `Stop` cancels the jobs' context and waits for running jobs
  up to the deadline you pass, then returns.
- **Per-process only.** The scheduler holds no state outside the process and
  takes no distributed lock, so several replicas each run their own copy of every
  job. When a job must run on exactly one replica, elect the leader yourself with
  a database or Redis lock. The scheduler intentionally depends on neither, and
  it has no admin API and persists nothing.

## Tests

The tests never wait for a schedule to fire. A `Job` is a plain function invoked
directly, and the decorated runner is driven through the scheduler's own entry so
the overlap guard, panic recovery and graceful stop are all checked
deterministically, with no `time.Sleep`.

```sh
go test -race ./internal/cron/...
```

## Wiring it into a long-running command

The scheduler must follow a long-running command — `serve`, in both the plain and
the Loom composition — and must not start anywhere else. That wiring touches
files the cron capability does not own, so it is proposed here and must be
applied by the owner of those files (`capabilities/http`, `capabilities/loom`,
`capabilities/base`). See `capabilities/cron/proposals/` in the template for the
exact snippets:

- `proposals/http_serve.snippet` — the plain `serve` command.
- `proposals/loom_graph.snippet` — the Loom graph's scheduler provider and
  lifecycle hooks.
- `proposals/base_runtime.snippet` — the base-app runtime seam that lets a
  capability attach work to the long-running commands.

Until that wiring exists, the scheduler is a library you start yourself; once it
exists, `serve` starts it and stops it with the process.
