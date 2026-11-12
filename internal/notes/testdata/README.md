# Upstream rendering fixtures

`upstream.json` contains 14 golden cases generated from the pure release-note
rendering and label-classification functions in the
[pinned upstream source](https://github.com/tiangolo/latest-changes-app/blob/40ba35e399a1c799029280832840f57cc6bf1c24/app/latest_changes.py).
The source itself is not bundled; the Go tests read only this JSON fixture.

Cases cover new documents, existing and historical sections, section ordering,
legacy entries, CRLF normalization, idempotency, invalid headings, and invalid
category combinations.

The adapted behavior and fixtures retain the original MIT notice in the root
[LICENSE](../../../LICENSE).
