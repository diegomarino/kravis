<!-- GENERATED FILE: do not edit by hand. Regenerate by running ddduck generate --root ../.. from this file's directory. -->

# Kravis (`model:kravis`)

A CRM with projects and tasks that agents and the people they work for use to record and follow relationships, deals and work, in files they own. It is built for complex deals in which several parties take part in different roles, such as advisory mandates with clients, counterparties, lenders and subcontracted firms.

## Domains

### Deals (`domain:deals`)

Follow each deal through its pipeline with every party involved, the roles each one plays, and the outcome.

#### Concepts

- `concept:deal` - Deal
- `concept:lost-reason` - Lost reason
- `concept:party` - Party
- `concept:party-role` - Party role
- `concept:pipeline` - Pipeline
- `concept:stage` - Stage

#### Interfaces

- None.

#### Active guarantees

- `DEALS-AC-01` - A general deal update cannot change the stage, and moving a deal to a lost stage without a lost reason fails with the allowed reasons.
- `DEALS-INV-01` - Every deal is in exactly one pipeline and in exactly one of its stages. The MVP has a single built-in pipeline whose default stages are prospect, engaged and closing (ongoing), won, and lost.
- `DEALS-INV-02` - A deal changes stage only through its stage transition, which may move it to any stage of its pipeline; a general update cannot change the stage.
- `DEALS-INV-03` - Moving a deal to a stage of the lost category requires a lost reason from the catalog. Moving it back to an ongoing stage clears the reason.
- `DEALS-INV-04` - Every deal records its holder's party role from the catalog.
- `DEALS-INV-05` - Each party of a deal is exactly one person or one organization. Each person or organization appears at most once among a deal's parties, with any number of party roles from the catalog. The deal's holder is never one of its parties.

### Directory (`domain:directory`)

Keep one shared record of the people and organizations Kravis deals with, and of which organizations are its own.

#### Concepts

- `concept:affiliation` - Affiliation
- `concept:organization` - Organization
- `concept:own-organization` - Own organization
- `concept:person` - Person
- `concept:title` - Title

#### Interfaces

- None.

#### Active guarantees

- `DIRECTORY-INV-01` - People and organizations have no holder: every holder shares the same people and organizations.
- `DIRECTORY-INV-02` - Creating a person with an email that already belongs to a person returns that person, and creating an organization with a domain that already belongs to an organization returns that organization.
- `DIRECTORY-INV-03` - Creating a person from a generic address, such as info@ or noreply@, succeeds with a warning, because such addresses do not belong to people.
- `DIRECTORY-INV-04` - A person has any number of affiliations, at most one per organization, each with a free-text title. An affiliation that ends is removed.

### Identity (`domain:identity`)

Know who acts in Kravis (humans and their agents) and attribute every write to an actor.

#### Concepts

- `concept:actor` - Actor
- `concept:agent` - Agent
- `concept:handler` - Handler
- `concept:human` - Human
- `concept:user` - User

#### Interfaces

- None.

#### Active guarantees

- `IDENTITY-AC-01` - A write without an actor fails with exit code 2 and a hint naming --actor and KRAVIS_ACTOR. kravis whoami shows the resolved actor, where it came from and its holder.
- `IDENTITY-INV-01` - Every user belongs to exactly one holder, which never changes.
- `IDENTITY-INV-02` - Every user is either a human or an agent, and that never changes.
- `IDENTITY-INV-03` - Every agent has exactly one handler: a live human of the agent's holder. In the MVP the handler never changes.
- `IDENTITY-INV-04` - Every write declares an actor, taken from the command flag, then the environment, then the local configuration. A write without an actor is refused; reads need none.
- `IDENTITY-INV-05` - The actor of a write is an existing user.
- `IDENTITY-INV-06` - A holder keeps a live human once it has one: deactivating or deleting the holder's last live human is refused before any change, so someone can still act for it.
- `WORKSPACE-INV-14` - Only a live user can act.

### Interactions (`domain:interactions`)

Record what happened with people and organizations, and what users know about them, linked to its external sources.

#### Concepts

- `concept:channel` - Channel
- `concept:direction` - Direction
- `concept:initiator` - Initiator
- `concept:interaction` - Interaction
- `concept:note` - Note
- `concept:participant` - Participant
- `concept:source` - Source
- `concept:source-reference` - Source reference

#### Interfaces

- None.

#### Active guarantees

- `INTERACTIONS-AC-01` - Recording the same email twice for the same holder returns existing the second time, and creating its follow-up twice also returns existing the second time.
- `INTERACTIONS-AC-02` - A person's outreach in a month is obtained by listing interactions by direction, initiator and date range.
- `INTERACTIONS-INV-01` - Every interaction has at least one participant, and each participant is exactly one person or one organization. The holder's own users are implied and need not be listed.
- `INTERACTIONS-INV-02` - Every interaction has a direction, outbound or inbound; outbound when none is given.
- `INTERACTIONS-INV-03` - An interaction's initiator, when recorded, is a user of its holder if the interaction is outbound and one of its participants if it is inbound.
- `INTERACTIONS-INV-04` - Adding a person with exactly one affiliation as a participant also adds that organization. With several affiliations nothing is added, and a warning lists them.
- `INTERACTIONS-INV-05` - An interaction may be linked to any number of deals and projects, all of its holder.
- `INTERACTIONS-INV-06` - Every interaction has one channel from the catalog.
- `INTERACTIONS-INV-07` - A source reference that identifies an interaction identifies at most one interaction per holder. Recording the same item again for the same holder returns the existing interaction; identifying references already split between two interactions of the same holder are a conflict.
- `INTERACTIONS-INV-08` - Kravis keeps source references and summaries, never a copy of the original content.
- `INTERACTIONS-INV-09` - A source reference to an unknown source is accepted with a warning and identifies an interaction only by exact equality.
- `INTERACTIONS-INV-10` - Every note has one or more subjects (people, organizations, deals or projects) and the holder of its actor; deals and projects among its subjects share that holder.

### Work (`domain:work`)

Track the projects and tasks that holders' humans and agents must get done.

#### Concepts

- `concept:assigned-agent` - Assigned agent
- `concept:assignee` - Assignee
- `concept:follow-up` - Follow-up
- `concept:project` - Project
- `concept:purpose` - Purpose
- `concept:status` - Status
- `concept:task` - Task

#### Interfaces

- None.

#### Active guarantees

- `WORK-AC-01` - Closing a project with live tasks fails and lists them. Once they are no longer live, the project closes.
- `WORK-AC-02` - A task list shows live tasks by default and filters by assignee, assigned agent, related record, overdue and due-before date.
- `WORK-INV-01` - A task has at most one work parent, a project or a deal. A task in a project belongs to that project's deal, if it has one. A task without a parent is an internal task of its holder.
- `WORK-INV-02` - A project is tied to at most one deal, and a deal may have any number of projects.
- `WORK-INV-03` - Every task has exactly one assignee: a live human of the task's holder.
- `WORK-INV-04` - A task's assigned agent, if any, is an agent whose handler is the task's assignee.
- `WORK-INV-05` - A task created from an interaction records that interaction and has a purpose, follow-up by default. There is at most one task per interaction and purpose.
- `WORK-INV-06` - A task created from an interaction without a parent takes the interaction's project or deal when there is exactly one; when there are several, the parent must be chosen.
- `WORK-INV-07` - A task changes status only through its status transition; a general update cannot change the status.
- `WORK-INV-08` - A task's or project's related records are people, organizations, deals or projects; related deals and projects share its holder. Related records take part in no other rule.
- `WORK-INV-09` - A project is closed and reopened by changing whether it is live. A project with live tasks cannot be closed, and a closed project accepts no new tasks.

### Workspace (`domain:workspace`)

Keep the workspace's records, catalogs and holder partition consistent, and expose them to agents through one contract.

#### Concepts

- `concept:catalog` - Catalog
- `concept:category` - Category
- `concept:holder` - Holder
- `concept:live` - Live
- `concept:workspace` - Workspace

#### Interfaces

- None.

#### Active guarantees

- `WORKSPACE-AC-01` - Outside a TTY every command prints exactly one JSON object: data and structured warnings on stdout, errors on stderr. An error is never mixed with data.
- `WORKSPACE-AC-02` - Every error has a stable code, a message, structured details, a retryable flag and, when the next step is not obvious from the details, a hint naming the next concrete action.
- `WORKSPACE-AC-03` - Exit codes are 0 for success (including existing and unchanged), 1 for an internal failure, 2 for usage or validation, 3 for not found, 4 for a conflict, 5 for storage unavailable and 6 when check finds issues.
- `WORKSPACE-AC-04` - Every write returns an outcome (created, existing, updated, unchanged or deleted) and the record's compact view. A create that finds a record with the same natural key returns existing; one whose data contradicts that record is a conflict. An update or transition that changes nothing returns unchanged and does not bump the version.
- `WORKSPACE-AC-05` - A list returns at most 50 records by default, always reports the total, and returns an opaque cursor. Each type has a fixed default order, declared in the schema, with the ID breaking ties. A record whose own sort value changed between two pages may be skipped or repeated; records created or deleted meanwhile do not move the others.
- `WORKSPACE-AC-06` - kravis schema describes each record type with the catalog values that apply to the actor's holder, the category of every status and stage, the value used when a field is omitted, the default order and the possible error codes.
- `WORKSPACE-AC-07` - Agent input with unknown fields is refused, while unknown fields found in a record on disk are kept when the record is rewritten.
- `WORKSPACE-AC-08` - When the record changes between an update's read and its write, the update is reapplied on the new version, and after 3 attempts it fails with VERSION_CONFLICT and exit code 4.
- `WORKSPACE-AC-09` - A read made with a declared actor states its holder filter in the response, and --all-holders removes it.
- `WORKSPACE-AC-10` - Deleting a record that another record refers to fails and lists the records that refer to it.
- `WORKSPACE-AC-11` - check reports its findings as JSON with a code, the affected record and a message, exits with code 6 when there are findings, and never changes a file.
- `WORKSPACE-AC-12` - init validates every input before writing. On an initialized workspace it returns existing when the organization and user match and fails with a hint otherwise. Rerunning an interrupted init completes what is missing, because kravis.json is written last.
- `WORKSPACE-AC-13` - A value outside its catalog (status, party role, purpose, lost reason or channel) is refused with INVALID_VALUE and the list of values allowed for the record's holder.
- `WORKSPACE-INV-01` - Every work record (deal, project, task, interaction, note) has exactly one holder, which is an own organization.
- `WORKSPACE-INV-02` - A work record takes its holder from its actor when it is created, and that holder matches the holder of every record it is structurally linked to.
- `WORKSPACE-INV-03` - A record's holder never changes.
- `WORKSPACE-INV-04` - An own organization that is the holder of any record cannot lose its own mark.
- `WORKSPACE-INV-05` - A catalog-valued field only accepts values from the catalog that applies to the record's holder. A value removed from the catalog stays on existing records, check reports it, and the next edit of the record must replace it. The one exception is a task's purpose, which never changes: a purpose in use is grandfathered. Removing it from the catalog only stops new tasks from taking it; the tasks that have it keep it, an update does not require replacing it, and check does not report it.
- `WORKSPACE-INV-06` - Categories are fixed and the same everywhere. Every status and stage belongs to exactly one category, and each record stores its category and its live flag next to its status or stage, consistent with the catalog.
- `WORKSPACE-INV-07` - A record's ID never changes, and its form is fixed per record type.
- `WORKSPACE-INV-08` - Every write increments the record's version and records which actor wrote it and when.
- `WORKSPACE-INV-09` - An update applies only to the version it read and is replaced only if that version is still current. With a storage that writes conditionally (an object store with If-Match) it never overwrites a concurrent change. The local filesystem leaves a window of under a millisecond between the last check and the replace, which is accepted; on a mounted bucket such as tigrisfs only one writer is supported.
- `WORKSPACE-INV-10` - Creating a record whose natural key already exists returns the existing record instead of creating a duplicate.
- `WORKSPACE-INV-11` - A record can be deleted only when no other record refers to it.
- `WORKSPACE-INV-12` - Reads made with a declared actor are limited to the actor's holder, and the response states that limit.
- `WORKSPACE-INV-13` - Every record has a live flag. Lists show only live records by default, and the others are included on request.

## Use Cases

### Correct a mistake (`use-case:correct-a-mistake`)

A user removes a record created by mistake, for example under the wrong holder, and creates it again with the right actor, leaving no tombstone behind.

- Requires: `IDENTITY-INV-04`, `IDENTITY-INV-05`, `WORKSPACE-INV-14`
- Preserves: `WORKSPACE-INV-03`, `WORKSPACE-INV-11`
- Establishes: `WORKSPACE-AC-10`, `WORKSPACE-AC-11`
- Interfaces: none

### Keep a note (`use-case:keep-a-note`)

A user records what they know about people, organizations, deals or projects as a note.

- Requires: `IDENTITY-INV-04`, `IDENTITY-INV-05`, `WORKSPACE-INV-14`
- Preserves: `WORKSPACE-INV-01`, `WORKSPACE-INV-02`, `INTERACTIONS-INV-10`
- Establishes: none
- Interfaces: none

### Manage projects and tasks (`use-case:manage-projects-and-tasks`)

A user creates projects and tasks, assigns each task to a human and optionally to one of their agents, moves tasks through their statuses and closes finished projects.

- Requires: `IDENTITY-INV-04`, `IDENTITY-INV-05`, `WORKSPACE-INV-14`
- Preserves: `WORK-INV-01`, `WORK-INV-02`, `WORK-INV-03`, `WORK-INV-04`, `WORK-INV-07`, `WORK-INV-08`, `WORK-INV-09`
- Establishes: `WORK-AC-01`, `WORK-AC-02`
- Interfaces: none

### Measure a person's outreach (`use-case:measure-outreach`)

A user or agent lists the interactions a person initiated in a month to assess their outreach.

- Requires: none
- Preserves: `INTERACTIONS-INV-02`, `INTERACTIONS-INV-03`
- Establishes: `INTERACTIONS-AC-02`
- Interfaces: none

### Prepare a briefing (`use-case:prepare-a-briefing`)

An agent gathers everything about a person or organization, and what is due today, from the basic reads and their filters.

- Requires: none
- Preserves: `WORKSPACE-INV-12`, `WORKSPACE-INV-13`
- Establishes: `WORKSPACE-AC-05`, `WORKSPACE-AC-06`, `WORKSPACE-AC-09`, `WORK-AC-02`
- Interfaces: none

### Record an interaction and its follow-up (`use-case:record-interaction-and-follow-up`)

An agent records an email or a meeting with its participants and source references, then creates the follow-up task, and can repeat either step safely.

- Requires: `IDENTITY-INV-04`, `IDENTITY-INV-05`, `WORKSPACE-INV-14`
- Preserves: `WORKSPACE-INV-01`, `WORKSPACE-INV-02`, `INTERACTIONS-INV-01`, `INTERACTIONS-INV-02`, `INTERACTIONS-INV-03`, `INTERACTIONS-INV-04`, `INTERACTIONS-INV-05`, `INTERACTIONS-INV-06`, `INTERACTIONS-INV-07`, `INTERACTIONS-INV-08`, `INTERACTIONS-INV-09`, `WORK-INV-05`, `WORK-INV-06`
- Establishes: `INTERACTIONS-AC-01`, `WORKSPACE-AC-01`, `WORKSPACE-AC-02`, `WORKSPACE-AC-03`, `WORKSPACE-AC-04`, `WORKSPACE-AC-07`, `WORKSPACE-AC-08`
- Interfaces: none

### Run a deal (`use-case:run-a-deal`)

A user creates a deal with its holder's party role, adds the parties and their party roles, moves it through its stages and closes it as won or lost with a lost reason.

- Requires: `IDENTITY-INV-04`, `IDENTITY-INV-05`, `WORKSPACE-INV-14`
- Preserves: `WORKSPACE-INV-05`, `WORKSPACE-INV-06`, `DEALS-INV-01`, `DEALS-INV-02`, `DEALS-INV-03`, `DEALS-INV-04`, `DEALS-INV-05`
- Establishes: `DEALS-AC-01`, `WORKSPACE-AC-13`
- Interfaces: none

### Set up a workspace (`use-case:set-up-a-workspace`)

A human initializes a workspace with its first own organization and user, then adds other own organizations, humans and their agents.

- Requires: none
- Preserves: `IDENTITY-INV-01`, `IDENTITY-INV-02`, `IDENTITY-INV-03`, `WORKSPACE-INV-04`
- Establishes: `WORKSPACE-AC-12`, `IDENTITY-AC-01`
- Interfaces: none

## Interfaces

## Relationships

- `rel:affiliation-links-person-organization`: `concept:person` -> `concept:organization` (`affiliated-with`) - An affiliation links a person to an organization and describes the person's title there.
- `rel:agent-works-under-handler`: `concept:agent` -> `concept:handler` (`answers-to`) - Each agent works under one human handler of its holder.
- `rel:assigned-agent-is-agent`: `concept:assigned-agent` -> `concept:agent` (`is-a`) - An assigned agent is an agent user acting in that role for a task.
- `rel:assignee-is-human`: `concept:assignee` -> `concept:human` (`is-a`) - An assignee is a human user acting in that role for a task.
- `rel:deal-held-by-holder`: `concept:deal` -> `concept:holder` (`held-by`) - Every deal is kept in the name of one holder.
- `rel:deal-in-stage`: `concept:deal` -> `concept:stage` (`is-in`) - A deal is in exactly one stage of its pipeline at a time.
- `rel:holder-is-own-organization`: `concept:holder` -> `concept:own-organization` (`is-a`) - A holder is an organization of the directory that carries the own mark.
- `rel:human-linked-to-person`: `concept:human` -> `concept:person` (`represented-by`) - A human user may be linked to the person who represents them in the directory, so they can take part in interactions and deals.
- `rel:initiator-is-participant`: `concept:initiator` -> `concept:participant` (`is-a`) - The initiator of an inbound interaction is one of its participants. Guarantees: `INTERACTIONS-INV-03`.
- `rel:initiator-is-user`: `concept:initiator` -> `concept:user` (`is-a`) - The initiator of an outbound interaction is a user of its holder. Guarantees: `INTERACTIONS-INV-03`.
- `rel:interaction-concerns-deal`: `concept:interaction` -> `concept:deal` (`concerns`) - An interaction may concern any number of deals of its holder.
- `rel:interaction-concerns-project`: `concept:interaction` -> `concept:project` (`concerns`) - An interaction may concern any number of projects of its holder.
- `rel:interaction-identified-by-source-reference`: `concept:interaction` -> `concept:source-reference` (`identified-by`) - Some source references identify their interaction, and the pair (reference, holder) is unique.
- `rel:note-about-deal`: `concept:note` -> `concept:deal` (`about`) - A note may be about deals of its holder. Guarantees: `INTERACTIONS-INV-10`.
- `rel:note-about-organization`: `concept:note` -> `concept:organization` (`about`) - A note may be about organizations. Guarantees: `INTERACTIONS-INV-10`.
- `rel:note-about-person`: `concept:note` -> `concept:person` (`about`) - A note may be about people. Guarantees: `INTERACTIONS-INV-10`.
- `rel:note-about-project`: `concept:note` -> `concept:project` (`about`) - A note may be about projects of its holder. Guarantees: `INTERACTIONS-INV-10`.
- `rel:participant-is-organization`: `concept:participant` -> `concept:organization` (`refers-to`) - A participant may be an organization from the shared directory. Guarantees: `INTERACTIONS-INV-01`.
- `rel:participant-is-person`: `concept:participant` -> `concept:person` (`refers-to`) - A participant may be a person from the shared directory. Guarantees: `INTERACTIONS-INV-01`.
- `rel:party-is-organization`: `concept:party` -> `concept:organization` (`refers-to`) - A party may be an organization from the shared directory. Guarantees: `DEALS-INV-05`.
- `rel:party-is-person`: `concept:party` -> `concept:person` (`refers-to`) - A party may be a person from the shared directory. Guarantees: `DEALS-INV-05`.
- `rel:party-plays-party-role`: `concept:party` -> `concept:party-role` (`plays`) - A party plays any number of party roles from the catalog, including none.
- `rel:project-tied-to-deal`: `concept:project` -> `concept:deal` (`tied-to`) - A project may be tied to one deal of its holder; a deal may have several projects.
- `rel:source-reference-points-into-source`: `concept:source-reference` -> `concept:source` (`points-into`) - A source reference points to one item of a source; each known source defines how its references are made canonical and which ones identify an interaction.
- `rel:stage-in-category`: `concept:stage` -> `concept:category` (`belongs-to`) - Every stage belongs to one fixed category: ongoing, won or lost.
- `rel:status-in-category`: `concept:status` -> `concept:category` (`belongs-to`) - Every task status belongs to one fixed category: open, waiting, done or canceled.
- `rel:task-assigned-to-assignee`: `concept:task` -> `concept:assignee` (`assigned-to`) - Every task has one human assignee and, optionally, an assigned agent working on the assignee's behalf.
- `rel:task-from-interaction`: `concept:task` -> `concept:interaction` (`created-from`) - A task may record the interaction it was created from. This is provenance, not grouping.
- `rel:task-in-deal`: `concept:task` -> `concept:deal` (`belongs-to`) - A task may belong directly to a deal of its holder. Guarantees: `WORK-INV-01`.
- `rel:task-in-project`: `concept:task` -> `concept:project` (`belongs-to`) - A task may belong to a project of its holder. Guarantees: `WORK-INV-01`.
- `rel:user-works-for-holder`: `concept:user` -> `concept:holder` (`belongs-to`) - Each user works for exactly one holder, and every write by that user takes this holder.

## Decisions

- `ADR-001`
