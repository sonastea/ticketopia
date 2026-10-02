# Documentation

The root [README](../README.md) covers the project overview and quick start.
Detailed setup and behavior live here.

- [Cache configuration](cache.md): backends, environment variables, expiration,
  fallback behavior, and NATS setup.
- [Recent changes](CHANGELOG.md): a short index of the latest change records.
- [All change records](changes/): history grouped by year, for targeted browsing
  or search.

## Documenting future changes

1. Add or update a focused feature guide and link it above. Guides describe the
   current behavior.
2. Add a concise record at `changes/YYYY/YYYY-MM-DD-short-title.md` for each
   notable change. Include the date, release (or **Unreleased**), a summary, and
   links to relevant guides. Update the release field when publishing a release.
3. Prepend a one-line link to `CHANGELOG.md`. Keep only the 20 most recent links;
   older records remain in their year directories.

Read guides for current setup and behavior. Search filenames or content when you
need history, then open the matching records. There is no need to load all records
for routine development.
