# Changelog

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
* **Interactions:** emails, meetings and calls with their participants, direction and initiator, identified by their Message-ID or provider reference, so recording the same one twice returns the existing one.
* **Tasks:** assigned to a human and optionally one of their agents, with statuses, due dates and follow-ups created from an interaction, safe to repeat.
* **Deals:** any number of parties, people or organizations, each with any number of party roles; the holder records its own party role; stages from a built-in pipeline, and losing a deal needs a lost reason.
* **Projects:** optionally tied to a deal; closing one with live tasks is refused, or `--cancel-tasks` cancels them first.
* **Notes:** about one or more people, organizations, deals and projects.
* **Briefings and outreach:** `--related-to` on tasks and notes, and interaction filters by direction, initiator and dates.
* **Built for agents:** every command prints one JSON object off a terminal; errors carry a code, `details` and a hint; `kravis schema <type>` describes each type; `kravis check` validates a workspace.
* **Skill:** `npx skills add diegomarino/kravis --skill kravis` teaches an agent the commands and workflows.
* **Releases:** binaries for macOS and Linux.
