# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

User decision (D-017, D-018): React site + separate Go server, PostgreSQL in Docker.
Delegated details chosen by Claude: Vite + React + TypeScript, React Router, TanStack Query; Go with chi, pgx + sqlc, goose migrations; Mailpit for local mail. Local run only for now (D-020).

## Users

Two sides, equally important (confirmed 2026-10-02):

- **Scientists looking for a position** (seekers): postgraduates, young researchers with a candidate degree, established researchers, teaching staff (ППС). They browse openings by field, region and conditions, keep a scientific profile instead of a generic CV, and apply with a cover letter, files and reference letters. Often on a phone between other work, sometimes at a desk preparing a full application.
- **Organizations that hire**: universities, research institutes and RAS institutes, research centers, R&D companies, technoparks. Inside them: an HR person who sees all vacancies, and unit heads (lab, department, chair) who publish positions for their own unit and review only their own applicants.

One account per person with several roles; a professor can both hire and look for a position, switching between "Ищу работу" and "Нанимаю" (D-004).

## Product Purpose

A job board for science in Russia: organizations publish research, teaching, early-career and science-management openings; scientists find them and apply. Success: a scientist finds a fitting position and gets an invitation; an organization fills a position from applicants it would not have reached otherwise.

## Positioning

Built for science, not adapted from a general job site. It understands degrees and academic titles, VAK specialties, competitive selections (конкурсы) with deadlines, grants and funding sources, publications and identifiers (ORCID, SPIN, Scopus, WoS). A scientist keeps a scientific profile, not a CV. Borrowed from international academic boards: reference letters submitted through the platform, career levels R1–R4, deadline reminders, rule-based "suitable for you" matching. Everything is free; there are no tariffs or paid placements (D-016).

## Operating Context

- Seekers: search with filters (field, region, format, conditions, required degree, level, organization type, position type, deadline), save searches and favorites, track applications and statuses, respond to invitations.
- Organizations: manage units and staff by invitation, publish vacancies through draft → published → closed/archive, review applications, send invitations (interview with date/time/link or address, contacts, or a request for the candidate's contacts).
- Recommenders upload letters by a one-time link without an account; the organization sees them, the seeker does not.
- Notifications on the site (bell) and by email.

## Capabilities and Constraints

- Russian interface; all strings in dictionaries, ready for English later (D-021).
- Product name is a working title, kept in one setting (D-001).
- Anyone can publish vacancies; no organization verification, no "verified" badges (D-003).
- No moderator role, no chat, no analytics for organizations, no monetization.
- Salary is optional on vacancies (D-014).
- Login: email + password; ORCID / Yandex / VK / Gosuslugi shown as "coming soon" (D-019).
- Personal data under 152-FZ: consent at registration; hosting in Russia when it goes public (D-020).
- Phone and desktop are equally important (D-023).
- The state portal ученые-исследователи.рф is not integrated for now (D-015).

## Brand Commitments

- Name: SciBox (working title, may change).
- Voice (confirmed 2026-10-02): businesslike but alive. Formal "вы", short and to the point, no bureaucratese, no familiarity. Example: «Откликнуться», «Вакансия закрыта 3 дня назад».

## Evidence on Hand

None yet. Demo content is fictional organizations and people with real Russian cities (D-022). Do not invent user counts, testimonials, partner logos or statistics; landing numbers come from the real database (slice 12).

## Product Principles

1. Speak the language of science: degrees, VAK specialties, competitions and grants are first-class fields, not free text.
2. Both sides at once: every feature is checked from the seeker's and the organization's seat.
3. Privacy is the seeker's choice: hidden / visible to organizations / public, and nothing leaks past it.
4. Free and honest: no paid placement, no fake verification, no invented numbers.
5. Works on a phone as well as at a desk.

## Accessibility & Inclusion

Users span ages and include senior academics: readable type sizes, sufficient contrast, full keyboard use. Target WCAG 2.2 AA.
