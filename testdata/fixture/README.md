# Test fixture: trimmed TfNSW data

Contains Transport for NSW data, © Transport for NSW, licensed under
[Creative Commons Attribution 4.0](https://opendata.transport.nsw.gov.au/datalicence).

- `gtfs.zip`: trips for a fixed set of lines (gym-side lines only, no home-specific data) on
  Sat 3, Sun 4 and Thu 8 Oct 2026, built from the complete GTFS bundle (non-train) and the Sydney Trains bundle (trains).
- `tu-*.pb`, `vp-*.pb`: GTFS-realtime trip updates and vehicle positions captured Sat 3 Oct 2026 at 12:37,
  filtered to the same lines.

Regenerate (overwrites; golden files will need `-update`):

    gymrouter make-fixture --dates 2026-10-03,2026-10-04,2026-10-08 --realtime \
      --lines "train T1,train T9,metro M1,bus 288,bus 291,bus 292,bus 533,bus 287,bus 52,bus 521,bus 523,bus 524,bus 281,bus 283,bus 160X,bus 207,bus 271,bus 194"
