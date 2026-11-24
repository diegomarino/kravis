# ADR-001 - Records are partitioned by holder, and a write takes its actor's holder

Kravis serves several own organizations from one workspace, and they share people and organizations. Every work record (deal, project, task, interaction, note) therefore belongs to exactly one holder. Each user, human or agent, works for exactly one holder, and a write takes its holder from its actor, so nobody can write into another organization by mistake. Reads made with a declared actor are limited to that holder, and the response says so.

A holder never changes. A record created under the wrong holder is deleted, which is allowed only when nothing refers to it, and created again by an actor of the right holder. Nothing is archived, so files hold no tombstones that would mislead agents reading them directly.

## Considered options

- Holder chosen per write, with users not tied to any holder: rejected because a user could write into the wrong organization without noticing.
- A user with several authorized holders: this avoids one identity per organization, but it brings back the per-write choice and its mistakes.
- Correcting the holder of a record that has no children: impossible once users are tied to one holder.
- Archiving mistakes: it leaves tombstones in the files that agents read.

## Consequences

- A human who works for several own organizations has one user per organization. All of them link to the same person.
- People and organizations have no holder and are shared. Users are not shared: each belongs to exactly one holder.
- Settings stay workspace-wide in the MVP, but catalog validation always resolves "the configuration for this holder", so per-holder settings can be added later.
