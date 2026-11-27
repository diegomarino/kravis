# Changelog

## [0.4.0](https://github.com/diegomarino/kravis/compare/v0.3.1...v0.4.0) (2026-11-27)


### Features

* init links the first user to a person ([#89](https://github.com/diegomarino/kravis/issues/89)) ([a507314](https://github.com/diegomarino/kravis/commit/a507314005c8932742407b0ace8194fd71b94056))
* text search ([#90](https://github.com/diegomarino/kravis/issues/90)) ([9456768](https://github.com/diegomarino/kravis/commit/945676855d97bba257d472a5a42deba2925d5c17))


### Fixes

* search hints, write responses and skill lines from the third e2e ([#95](https://github.com/diegomarino/kravis/issues/95)) ([273e323](https://github.com/diegomarino/kravis/commit/273e3231ae4738cdd9ba32d144592d73793d10f4))
* the briefing recipe asks for a person's affiliations ([#98](https://github.com/diegomarino/kravis/issues/98)) ([6bb1a0c](https://github.com/diegomarino/kravis/commit/6bb1a0ce1ada467f97ac3ac2b4df2f7e15f02d00))


### Documentation

* the closing check of increment 3 ([#92](https://github.com/diegomarino/kravis/issues/92)) ([5bebff8](https://github.com/diegomarino/kravis/commit/5bebff8afbc8350ae2d8cd9fcf40cfad0c07403d))
* the contract of text search ([#87](https://github.com/diegomarino/kravis/issues/87)) ([a202480](https://github.com/diegomarino/kravis/commit/a2024807a3f8db9a3c086be3586b6a69b3918929))

## [0.3.1](https://github.com/diegomarino/kravis/compare/v0.3.0...v0.3.1) (2026-11-25)


### Fixes

* skill and CLI edges found by the second end-to-end run ([#85](https://github.com/diegomarino/kravis/issues/85)) ([a387444](https://github.com/diegomarino/kravis/commit/a387444328116e57956ff581456a6e150400c3d0))


### Documentation

* the MVP and the license in the changelog ([#79](https://github.com/diegomarino/kravis/issues/79)) ([024ba8e](https://github.com/diegomarino/kravis/commit/024ba8e6b94fe586568bf581c5e7ff8e6d9b1230))

## [0.3.0](https://github.com/diegomarino/kravis/compare/v0.2.0...v0.3.0) (2026-11-25)


### Features

* person briefings include follow-ups, and lists echo every filter ([#72](https://github.com/diegomarino/kravis/issues/72)) ([5b354a4](https://github.com/diegomarino/kravis/commit/5b354a4c03ed5853afffd422624b406f1cf9ecaf))


### Fixes

* a delete cannot erase what a concurrent re-create wrote ([#78](https://github.com/diegomarino/kravis/issues/78)) ([c186b62](https://github.com/diegomarino/kravis/commit/c186b6292d6c6c4c58b75723287ae56e5f3c48dd))
* guards fail closed, terminal lists show scope, small input fixes ([#75](https://github.com/diegomarino/kravis/issues/75)) ([1b16460](https://github.com/diegomarino/kravis/commit/1b164600e647185acc98c7030b9c22b0452927e3))
* last live user, nested unknown fields, metadata, unsluggable names ([#77](https://github.com/diegomarino/kravis/issues/77)) ([882a292](https://github.com/diegomarino/kravis/commit/882a2923c36a9533707f4293adce45fec4a8feea))
* schema shows default catalogs; skill covers the e2e gaps ([#71](https://github.com/diegomarino/kravis/issues/71)) ([0c3dc0c](https://github.com/diegomarino/kravis/commit/0c3dc0ce8aca25c6cb0f7a28b97911443767986a))


### Documentation

* a better README ([#69](https://github.com/diegomarino/kravis/issues/69)) ([39c73d1](https://github.com/diegomarino/kravis/commit/39c73d14f5fd24f90a69a1e8c7c0bc0db2334fa9))
* contract, help and skill match the CLI ([#74](https://github.com/diegomarino/kravis/issues/74)) ([a32cdd5](https://github.com/diegomarino/kravis/commit/a32cdd506b7756dd5634200cb80a70ca20556ac0))
* install the skill from the release tag, state platforms ([#76](https://github.com/diegomarino/kravis/issues/76)) ([fb1f835](https://github.com/diegomarino/kravis/commit/fb1f8359385502679ae2bb261dd73755caddc3ff))


### Miscellaneous

* MIT license; release archives include LICENSE and THIRD_PARTY_NOTICES.md ([#73](https://github.com/diegomarino/kravis/issues/73))

## [0.2.0](https://github.com/diegomarino/kravis/releases/tag/v0.2.0) (2026-11-25)

The MVP. Earlier work was never released on its own; this entry sums it up.

### Features

* **Workspace:** `kravis init` creates a workspace in a folder you own, with your organization as the holder and you as its first user. One JSON file per record, long text in a Markdown file next to it.
* **Users and agents:** humans and agents are users; each agent works under a human, its handler, and every record says who wrote it (`--actor`, `KRAVIS_ACTOR`, `kravis whoami`).
* **Directory:** people (found by email) and organizations (found by domain), shared by every holder; a person can be affiliated with several organizations.
* **Interactions:** emails, meetings and calls with their participants, direction and initiator; an email recorded with its Message-ID is recorded once, so repeating it returns the existing one.
* **Tasks:** assigned to a human and optionally one of their agents, with statuses, due dates and follow-ups created from an interaction, safe to repeat.
* **Deals:** any number of parties, people or organizations, each with any number of party roles; the holder records its own party role; stages from a built-in pipeline, and losing a deal needs a lost reason.
* **Projects:** optionally tied to a deal; closing one with live tasks is refused, or `--cancel-tasks` cancels them first.
* **Notes:** about one or more people, organizations, deals and projects.
* **Briefings and outreach:** `--related-to` on tasks and notes, and interaction filters by direction, initiator and dates.
* **Built for agents:** every command prints one JSON object off a terminal; errors carry a code, `details` and a hint; `kravis schema <type>` describes each type; `kravis check` validates a workspace.
* **Skill:** `npx skills add diegomarino/kravis --skill kravis` teaches an agent the commands and workflows.
* **Releases:** binaries for macOS and Linux.
