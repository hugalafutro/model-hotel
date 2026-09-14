One file per scrape job, named as `prometheus.yml` references it (`mh1`, `mh2`, ...),
holding that member's `METRICS_TOKEN` on a single line, mode 0600. Until a member
has a dedicated `METRICS_TOKEN`, its admin token opens `/metrics` and works here too.
The files are gitignored; only this note is tracked.
