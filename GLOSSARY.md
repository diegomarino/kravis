# Kravis

A CRM with projects and tasks, built so that agents and the people they work for can record and follow relationships, deals and work.

## Language

**Workspace**:
The body of records that Kravis manages, together with its configuration and catalogs.
_Avoid_: Space, vault, store, repository, instance

**Catalog**:
A list of names that a workspace configures for itself, such as task statuses, party roles, purposes, lost reasons or channels.
_Avoid_: Enum, lookup, settings

**Live**:
Whether a record is still in use: an ongoing deal, an open or waiting task, a user who can still act. Closed deals and tasks are not live, but they are kept.
_Avoid_: Active, archived, open, current

### People and users

**Person**:
An individual with whom a relationship is kept.
_Avoid_: Contact, lead

**User**:
Someone who operates Kravis on behalf of one holder: writes records and can receive work. A user is either a human or an agent.
_Avoid_: Operator, account, member

**Actor**:
The user who performs a given write.
_Avoid_: Author, caller, requester

**Human**:
A user who is a human being, not an agent. A human may be linked to their own person, so that they can take part in interactions and deals.
_Avoid_: Person (for the user), member

**Agent**:
A non-human user with its own identity that works under a handler.
_Avoid_: Bot, assistant

**Handler**:
The human user who answers for what an agent does.
_Avoid_: Principal, sponsor, owner, supervisor, operator, responsible

**Assignee**:
The human user who answers for a task getting done. They may hand its execution to one of their agents.
_Avoid_: Owner, responsible

**Assigned agent**:
The agent, if any, that carries out a task on behalf of its assignee.
_Avoid_: Delegate, worker, executor

### Organizations

**Organization**:
A company, public body or other group that Kravis keeps track of as one unit.
_Avoid_: Company, account, firm

**Affiliation**:
The link between a person and an organization, described by the person's title there.
_Avoid_: Employment, membership, position

**Title**:
How a person's place in an organization is described, written freely, such as CFO or board member.
_Avoid_: Role, position, job

**Own organization**:
An organization whose work is managed in Kravis and which can therefore be a holder.
_Avoid_: Internal organization, entity, house, home organization, self

**Holder**:
The own organization in whose name a deal, project, task, interaction or note is kept, and on whose behalf a user works.
_Avoid_: Owner, entity, company, account, tenant, instance, master

### Deals

**Deal**:
A potential or actual transaction that a holder follows through the stages of a pipeline.
_Avoid_: Opportunity, operation

**Party**:
An organization or person that takes part in a deal, in any number of party roles.
_Avoid_: Participant (that is for interactions), stakeholder, association, counterparty (as the generic term)

**Party role**:
The part a party plays in a particular deal, such as client or advisor.
_Avoid_: Role (on its own), type, function, title, capacity

**Pipeline**:
A sequence of stages that a deal moves through.
_Avoid_: Funnel, workflow, process

**Stage**:
A named step of a pipeline. A deal is in exactly one stage at a time. Every stage belongs to a category.
_Avoid_: Phase, step, status

**Lost reason**:
Why a deal was lost, chosen from the workspace's catalog, such as price or timing.
_Avoid_: Loss cause, close reason

### Work

**Project**:
A finite or continuing effort that groups a holder's tasks and interactions, optionally tied to a deal.
_Avoid_: Initiative, workstream, engagement, matter

**Task**:
A piece of work with a human assignee and a status.
_Avoid_: To-do, action item, activity

**Purpose**:
Why a task exists, chosen from the workspace's catalog of purposes, such as follow-up, ping or expand. A task created from an interaction always has one.
_Avoid_: Reason, intent, kind, type

**Follow-up**:
A task whose purpose is to act on what happened in an interaction.
_Avoid_: Next step, reminder

**Status**:
A named situation that a workspace defines for its tasks. A task has exactly one status at a time. Every status belongs to a category.
_Avoid_: State, phase, stage

**Category**:
The fixed kind that a task status or a deal stage belongs to. Categories are the same in every workspace, while status and stage names vary. The categories for task statuses are open, waiting, done and canceled; those for deal stages are ongoing, won and lost.
_Avoid_: Class, type, state

### Record of what happened

**Interaction**:
Something that happened with people or organizations through a channel, recorded with its date and a summary.
_Avoid_: Activity, engagement, touchpoint, event, communication

**Channel**:
The means by which an interaction took place, chosen from the workspace's catalog, such as email, meeting, call or message.
_Avoid_: Type, kind, medium

**Participant**:
A person or organization involved in an interaction.
_Avoid_: Attendee, party, counterpart

**Direction**:
Which side started an interaction: ours (outbound) or the other one (inbound).
_Avoid_: Flow, way

**Initiator**:
The user or participant who started an interaction. It is not necessarily the actor who recorded it.
_Avoid_: Sender, organizer, owner, author

**Source**:
An external service, such as Gmail, Calendar, Granola, Drive or the web, that Kravis can point into.
_Avoid_: Provider, integration, connector, system, origin

**Source reference**:
A pointer to an item in a source, such as a Gmail message or a Drive file, kept so that the original can be found again.
_Avoid_: Link, attachment, external reference

**Note**:
Something a user knows about a person, organization, deal or project that did not happen as an interaction, recorded as free text.
_Avoid_: Memo, comment, annotation
