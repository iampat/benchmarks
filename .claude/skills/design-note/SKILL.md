---
name: design-note
description: Write or update any documentation or technical prose — design notes, architecture notes, READMEs, PR descriptions, release notes. Use for all technical writing to keep the style and structure consistent.
---

# Technical writing and design notes

The writing rules and habits in this skill apply to all documentation and
technical prose. The Structure section and the template apply to design notes
in `docs/design/`.

A design note records a decision and the reason for it. The reader arrives months
later with one question. Answer that question near the top.

Write Markdown in `docs/design/`. Markdown renders on GitHub, in an editor, and in
a terminal. It also diffs one line at a time, so a reviewer can comment on a
sentence. Do not write HTML. Generate HTML from the Markdown if a published site
is needed later.

## Assume a working software engineer

The reader knows the language, the protocol, and the common patterns. Never
explain a `oneof`, a status code, a map, or a retry. Explaining a known thing
wastes the reader's attention and buries the part that is specific to us.

Write about the choice, not the mechanism. "Patch and delete join the same
`oneof`" is enough. Why a `oneof` holds one field at a time is not.

Three more habits waste the same attention.

- Do not justify an obvious fact. The reader sees that a message is empty. A
  sentence calling the emptiness deliberate, or naming what it reserves, adds
  nothing.
- Do not narrate incompleteness. "This section has no messages yet" and "four
  operations are required" restate what the reader sees. Mark the item as future
  work and list what is missing.
- Do not explain implementation. How a prefix becomes a range, or why a square
  root is monotonic, belongs to the engine, not to the contract.

Apply one test. Delete the sentence. If the reader loses nothing they could not
work out, the sentence was padding.

## Two kinds of prose

A design note contains normative prose and rationale prose. They follow different
rules.

Normative prose states what the system does. It covers the model, the message
shapes, the invariants, and the limits. Apply the writing rules below without
exception. Ambiguity here becomes a defect later.

Rationale prose explains why. It weighs one option against another and names a
cost. Keep it plain, but let a sentence carry a subordinate clause when the
argument needs one. A rule that forbids "because" and "which means" removes the
connective tissue of an argument, and every claim then lands with equal weight.

Rationale prose is still short. Three sentences per decision is usually enough.

## Writing rules

These rules follow ASD-STE100 Simplified Technical English, Issue 9. Apply them
strictly to normative prose and loosely to rationale prose.

- Put the important information first in the sentence. Write a condition before
  its instruction.
- Keep an instruction to 20 words and a description to 25. Split a longer
  sentence.
- Write one idea per sentence. Keep a paragraph to one topic and a maximum of
  six sentences.
- Use active voice. Use passive only when the agent is unknown.
- Use simple tenses only. Describe what the system does, not what it will do.
- Use third person for a description. Use the imperative for an instruction.
- Do not write "I". Write "we" only to give a recommendation.
- Use one term for one concept. Never change a term for variety.
- Use a list when a sentence names three or more items.
- Do not use a semicolon. Write two sentences.
- Do not use the "-ing" form of a verb, except in a technical noun or a heading.
- Do not omit an article or the conjunction "that" to shorten a sentence.
- Do not use a phrasal verb. Write "extinguish", not "put out".
- Write "for example", not "e.g.". Write "and so on", not "etc.".
- Spell out an acronym at first use, then use the acronym.
- Do not use jargon, idiom, or an informal expression.
- State a trade-off in one sentence. Do not argue both sides at length.
- Delete a sentence that repeats the sentence before it.

`word-list.md` in this skill directory gives the substitutions from the STE
dictionary that software prose needs most, with sample rewrites. Read it when
you write or review prose.

## Prior art is evidence, not a blueprint

This project studies other systems and takes what is useful. Present our design on
its own terms. Do not present it as a set of differences from another system.

A note that frames every decision against one reference system makes that system
the spine of our architecture. It also implies that every choice the other system
made was a live option for us, which is often false. A transport that dictates an
answer is not a decision at all.

Cite another system in one sentence, where the decision sits, and only when it
carries information we do not have. Published operating experience counts. A
versioned name that marks a migration they could not perform counts. Do not open a
section for it, and never cite a system to justify a choice the medium forces.

## Fold the reasoning into the decision

Every decision names its cost, and the alternative it beat, in the section that
makes it. A choice with no cost is usually a choice that is not yet understood.

Do not collect that reasoning into separate Alternatives, Prior art, or Failure
modes sections. A reader who is looking at a decision should not have to find its
rationale three screens away, and a note that answers the same question twice gets
one of them out of date.

An unresolved question is the exception worth marking. Use `CONSIDER(ali):` inline
where the decision would go.

## Structure

Use this order. Skip a section that has no content.

| Section     | Content                                               |
| ----------- | ----------------------------------------------------- |
| Status      | Draft, accepted, or superseded. The date.             |
| Problem     | What the work must achieve. The constraints.          |
| Goals       | What success looks like.                              |
| Non-goals   | What sits outside the objective of the document.      |
| Future work | What belongs here later, and what defers it.          |
| Model       | The core objects and how they relate.                 |
| Design      | One section per topic. Name the topic, not the answer.|

A non-goal sits outside the objective of the document. It never enters the
document. Future work is related and belongs here eventually. It waits because
the document must stay manageable, or because the decision is too early.

A reader needs both lists. Future work shapes the messages today. A non-goal
never will.

Define a term before another section uses it. Name the core objects in the Model
section. A reader who meets "namespace" in a request message must not
reconstruct the term from context.

A heading names a topic. Write "Identifiers", not "One identifier type, ordered by
encoding". State the decision in the first sentence of the section. A reader scans
headings to find a topic, not to collect conclusions.

Put the open questions at the end, never in a footnote. An unresolved question is
the most valuable part of a draft.

## Diagrams and sketches

Pick the smallest view that makes the point. Put each visual next to the
sentence it supports, not in a gallery at the end.

Match the view to the content:

- An algorithm or a state change: pseudocode.
- Runtime flow: a call tree, one call per line, indented.
- Ownership or layout: a shallow file tree, one comment per entry.
- The shape of data or a system: a box diagram.
- A measurement: a bar chart, with the number at the end of the bar.

```
  sign-up form                checkout form
  ┌──────────────┐            ┌──────────────┐
  │ email, pass  │            │  cart, card  │
  └──┬────────┬──┘            └──┬────────┬──┘
     │        │                  │        │
     ▼        ▼                  ▼        ▼
  validate  account          validate  order
  (client)  (server)         (client)  (server)
```

```
  v1 ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  51.6 GiB
  v2 ║░░░                                5.2 GiB
```

Show a change to an existing shape as a diff on that shape — a file tree, a
call tree, or pseudocode. A diff shows the delta without two full diagrams.

```diff
 server/
 ├── routes.go      # maps URLs to handlers
+├── sessions.go    # tracks signed-in users
 └── handlers.go
```

Draw with box-drawing characters in a fenced code block. A fenced block
renders as monospace on GitHub, in an editor, and in a terminal. Use these
characters: `┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼ ─ │ ▶ ▼ ◀ ▲`.

Cap every diagram at 80 columns. A wider diagram scrolls sideways on GitHub and
wraps in a terminal.

A diagram that does not fit these limits is a signal, not a formatting
problem. It usually means the design has too many interacting parts. First
simplify the design. Then split the diagram into one small diagram per
decision. Use Mermaid only when the concept resists both. Treat a Mermaid
diagram as a flag for design review.

## Code in a design note

Show a type or a message definition when the reader needs the exact shape. Keep
the snippet to the fields under discussion. Cut the imports and the boilerplate.

A design note is not a source file. Do not paste a whole file into it.

## A stated limit is a promise

A limit in a contract binds every client. Do not state a limit that nobody
measured. An unmeasured limit is a guess, and a client builds against it. This
covers sizes, counts, depths, and rates.

## Field names in a public schema

No field name is a keyword in a target language of the project. Some generators
rename a colliding field, and others do not. Avoid the collision at the source.

## Lint

Run `.claude/skills/design-note/lint.sh <file>` on every document before you
finish. It checks the mechanical rules: semicolons, Latin abbreviations,
contractions, sentence length, and common non-approved words. Its word counts
are approximate, so a flagged sentence can be legal under the counting rules.
The checklist below covers the judgment rules.

## Checklist

- The note opens with the problem, not with the answer.
- Every heading names a topic. No heading states a conclusion.
- The design reads on its own terms. Another system is not the spine.
- Every decision names its cost, in the section that makes it.
- Non-goals and future work are separate sections.
- The Model section defines every term a later section uses.
- Every stated limit comes from a measurement.
- No field name is a keyword in a target language.
- Deleting any sentence loses something. No sentence explains the obvious.
- Every diagram fits in 80 columns and sits next to the text it supports.
- No Mermaid diagram survives without a simplification attempt first.
- Every open question carries a `CONSIDER(ali):` marker.
- No normative sentence is longer than 25 words.
