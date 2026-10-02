# Documentation guidance

- Load only documentation relevant to the current task. Use `docs/README.md` to
  locate feature guides.
- Read change history only when historical context is needed. Search filenames
  or content to find relevant records; do not bulk-read `docs/changes/`.
- For notable changes, update the relevant guide and the matching feature history
  at `docs/changes/<feature>.md`. Combine related ongoing work in its existing
  dated **Unreleased** entry. Link to guides instead of copying their contents.
- Keep the root README goal checklist in sync with delivered milestones. Check
  items only when their user-facing behavior is implemented and verified.
- Keep `docs/CHANGELOG.md` to one-line links for the 20 most recently updated
  feature histories, newest first, with only one link per feature. Retain feature
  files when their links age out of this index.
- Keep feature histories and individual archive files within 100 lines. Move
  older completed entries to `docs/changes/archive/<feature>/YYYY-part-NN.md`
  when needed, preserving their dates and releases. Link the feature history to
  its archive directory once it exists; read only relevant entries.
