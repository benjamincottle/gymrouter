# Test fixture: trackwork

Contains Transport for NSW data, © Transport for NSW, licensed under
[Creative Commons Attribution 4.0](https://opendata.transport.nsw.gov.au/datalicence).

- `gtfs.zip`: trips for a few lines around Bondi Junction and Waterloo on Sat 10 Oct 2026, when buses replace T4
  trains between Bondi Junction and Central. The T4 replacement buses (20T4, 23T4, …) come in with `train T4`.

Regenerate (overwrites):

    gymrouter make-fixture --dates 2026-10-10 --out testdata/trackwork \
      --lines "train T4,train T2,metro M1,bus 392,bus 320,bus 304,bus 343"
