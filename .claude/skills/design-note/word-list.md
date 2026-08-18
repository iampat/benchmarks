# STE word list for software prose

Substitutions from the ASD-STE100 dictionary that software prose needs most.
Read this file when you write or review documentation. One meaning per word:
after you select a term, keep it.

## Verbs

| Do not write              | Write                                          |
| ------------------------- | ---------------------------------------------- |
| carry out, perform        | do                                             |
| utilize, employ, leverage | use                                            |
| ensure, verify, assure    | make sure that                                 |
| check (as a verb)         | do a check of ("check" is a noun in STE)       |
| test (as a verb)          | do a test of ("test" is a noun in STE)         |
| damage (as a verb)        | cause damage to                                |
| begin, commence, initiate | start                                          |
| terminate                 | stop                                           |
| modify                    | change                                         |
| indicate                  | show                                           |
| detect                    | find (a sensor "detects": technical verb)      |
| follow (instructions)     | obey                                           |
| fails to start            | does not start                                 |
| is required to            | must                                           |
| required                  | necessary                                      |

## Function words and connectors

| Do not write                        | Write                  |
| ----------------------------------- | ---------------------- |
| however                             | but                    |
| therefore, hence                    | thus                   |
| consequently                        | as a result            |
| additionally, furthermore, moreover | also                   |
| prior to                            | before                 |
| subsequent to                       | after                  |
| within                              | in                     |
| upon                                | on                     |
| via                                 | through, with          |
| in order to                         | to                     |
| whilst                              | while                  |
| regarding, concerning               | about                  |
| certain (a quantity)                | some                   |
| acceptable                          | permitted              |
| appropriate                         | applicable, correct    |
| the following table                 | the table that follows |
| above, below (for limits)           | more than, less than   |
| should                              | must, or can           |
| e.g.                                | for example            |
| i.e.                                | that is                |
| etc.                                | and so on, or omit it  |

## Software terms that need no substitution

Rule 1.12 approves computer-process technical verbs: boot, click, copy, delete,
disable, download, drag, enable, encrypt, install, load, paste, reboot, save,
scroll, sort, update, upgrade, upload. Rule 1.5, category 19, approves
technical nouns such as: backup, database, field, file, firewall, interface,
memory, metadata, network, operating system, search engine, token.

## Sample rewrites

### Descriptive

Before:

> The WAL ensures that acknowledged writes are never lost; prior to serving
> reads, the engine replays it in order to reconstruct the memtable.

After:

> The write-ahead log (WAL) makes sure that each acknowledged write survives a
> crash. Before the engine serves reads, it replays the WAL to reconstruct the
> memtable.

### Procedural

Before:

> The benchmark suite should be run and the results compared against the
> baseline prior to merging; regressions exceeding 5% need to be investigated.

After:

> Before you merge, run the benchmark suite. Compare the results with the
> baseline. If a result is more than 5 percent slower, find the cause.

### Passive voice and "-ing" clauses

Before:

> Compaction is triggered when the segment count exceeds the threshold,
> merging smaller segments into larger ones, thereby reducing read
> amplification.

After:

> Compaction starts when the segment count is more than the threshold. It
> merges small segments into larger segments. This decreases read
> amplification.
