# The old format, kept to be migrated FROM

These are the fixture corpora as `*.kfacts.md`, moved out of `examples/` when
facts became an assertion store (`plan/facts.md`).

They are not a second test corpus. `examples/` is the corpus, and it now holds
what kgraph actually reads — an exported log. These exist because **migration is
a one-way import and needs a sample of the old format to import**, and a
migration tested against a fixture written for the test would only exercise what
its author remembered.

`TestMigratingTheFixtureCorpusChangesNothing` reads them, migrates them, and
asserts every node's `SemHash` matches what the export in `examples/` produces.
That is what makes the migration checkable rather than merely claimed.

Do not add facts here. Add them in `examples/`, in the format that is read.
