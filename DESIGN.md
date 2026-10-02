---
name: SciBox
description: A science job board set like a serious journal, with one yellow highlighter for the deadline.
colors:
  paper: "#ffffff"
  wash: "#f6f4f1"
  wash-deep: "#ece9e4"
  ink: "#1f1d1a"
  ink-body: "#38352f"
  ink-soft: "#5d5a55"
  line: "#e4e1dc"
  edge: "#85807a"
  ink-blue: "#1c2c66"
  ink-blue-deep: "#121e4d"
  blue-wash: "#eceefa"
  marker-yellow: "#f7d559"
  ok: "#1e6b45"
  ok-wash: "#e6f3ec"
  fail: "#a3261b"
  fail-deep: "#7f1c13"
  fail-wash: "#fbebe9"
  on-ink: "#ffffff"
  on-ink-soft: "#d9d6d1"
typography:
  display:
    fontFamily: "Literata Variable, Literata, PT Serif, Georgia, serif"
    fontSize: "clamp(1.875rem, 1.45rem + 1.9vw, 2.625rem)"
    fontWeight: 600
    lineHeight: 1.15
    letterSpacing: "-0.02em"
  headline:
    fontFamily: "Literata Variable, Literata, PT Serif, Georgia, serif"
    fontSize: "1.625rem"
    fontWeight: 600
    lineHeight: 1.22
    letterSpacing: "-0.015em"
  title:
    fontFamily: "Literata Variable, Literata, PT Serif, Georgia, serif"
    fontSize: "1.3125rem"
    fontWeight: 600
    lineHeight: 1.28
    letterSpacing: "-0.015em"
  abstract:
    fontFamily: "Literata Variable, Literata, PT Serif, Georgia, serif"
    fontSize: "1.125rem"
    fontWeight: 400
    lineHeight: 1.6
  body:
    fontFamily: "Golos Text Variable, Golos Text, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.55
  label:
    fontFamily: "Golos Text Variable, Golos Text, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 600
    lineHeight: 1.35
  caption:
    fontFamily: "Golos Text Variable, Golos Text, system-ui, sans-serif"
    fontSize: "0.8125rem"
    fontWeight: 500
    lineHeight: 1.5
rounded:
  sm: "6px"
  md: "10px"
  pill: "999px"
spacing:
  s-1: "4px"
  s-2: "8px"
  s-3: "12px"
  s-4: "16px"
  s-5: "24px"
  s-6: "32px"
  s-7: "48px"
  s-8: "72px"
components:
  button-primary:
    backgroundColor: "{colors.ink-blue}"
    textColor: "{colors.on-ink}"
    rounded: "{rounded.md}"
    padding: "8px 20px"
    height: "44px"
  button-primary-hover:
    backgroundColor: "{colors.ink-blue-deep}"
  button-secondary:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    height: "44px"
  button-quiet:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink-blue}"
    rounded: "{rounded.md}"
  button-danger:
    backgroundColor: "{colors.fail}"
    textColor: "{colors.on-ink}"
    rounded: "{rounded.md}"
  button-disabled:
    backgroundColor: "{colors.wash-deep}"
    textColor: "{colors.ink-soft}"
  text-field:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "8px 14px"
    height: "44px"
  text-field-error:
    backgroundColor: "{colors.fail-wash}"
    textColor: "{colors.ink}"
  tag:
    backgroundColor: "{colors.wash}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "1px 8px"
  tag-accent:
    backgroundColor: "{colors.blue-wash}"
    textColor: "{colors.ink-blue}"
    rounded: "{rounded.sm}"
  chip:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "4px 14px"
    height: "36px"
  chip-selected:
    backgroundColor: "{colors.blue-wash}"
    textColor: "{colors.ink-blue}"
  toast:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.on-ink}"
    rounded: "{rounded.md}"
    padding: "12px 12px 12px 16px"
  modal:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
---

# Design System: SciBox

## Overview

**Creative North Star: "The Journal"**

A vacancy is set like an article in a serious science journal: serif title, organization line, abstract, a row of facts. The page is pure white, the ink is warm near-black, actions and links are one deep ink-blue, and the single accent is a marker-yellow highlighter that appears only on a deadline that needs attention. Entries are separated by hairline rules, not boxed cards. The world refuses the generic job-board card grid with logo tiles and a bright blue button.

Density is calm and readable: generous line height, a 68ch reading measure, a 72rem content column. Phone and desktop are equal; the same entry reflows so the deadline stays visible before the abstract on a phone. There is no dark theme. Depth is typographic and linear (rules, weight, whitespace), not shadowed.

**Key Characteristics:**
- Serif for what is read (titles, abstracts, lead paragraphs, wordmark); sans for what is operated (interface, facts, forms).
- One action color (ink-blue) and one accent (marker yellow), never mixed in roles.
- Hairlines (1px) between entries and sections; one black rule under the result count.
- Square-ish corners (6px small, 10px controls and panels); pills only for filter chips.
- Shadow only on floating layers (modal, toast, combobox list) and as the focus ring.

## Colors

A white sheet, warm ink, one blue, one yellow; status colors are muted and used only for state.

### Primary
- **Ink Blue** (#1c2c66): primary buttons, links, the wordmark, selected chip text, focus ring, caret. Hover deepens to **Ink Blue Deep** (#121e4d). **Blue Wash** (#eceefa) is its tint for the selected chip, accent tag, active combobox option, and quiet-button hover.

### Secondary
- **Marker Yellow** (#f7d559): the only accent. It highlights an urgent deadline date, underlines the active nav item and active role tab (3px), colors text selection, and tints the info icon on dark toasts.

### Neutral
- **Paper** (#ffffff): page, fields, modal, secondary buttons.
- **Wash** (#f6f4f1) and **Wash Deep** (#ece9e4): footer, tags, hover fills, disabled controls, skeleton shimmer.
- **Ink** (#1f1d1a): headings, key facts, toast background. **Ink Body** (#38352f): abstracts. **Ink Soft** (#5d5a55): secondary lines, placeholders, labels' flags.
- **Line** (#e4e1dc): hairlines between entries and sections. **Edge** (#85807a): control borders, held at about 3.9:1 on white so fields are findable.
- **Status:** OK (#1e6b45) with OK Wash (#e6f3ec); Fail (#a3261b), Fail Deep (#7f1c13) and Fail Wash (#fbebe9) for errors and the danger button. On dark toasts: On Ink (#ffffff), On Ink Soft (#d9d6d1), with lighter green (#8fdcb2) and red (#ffb3aa) icon tints defined in tokens.

### Named Rules
**The One Highlighter Rule.** Marker yellow marks what must be done soon, and the current place in navigation. It is never a button fill, a background panel, or decoration.
**The One Action Blue Rule.** Every clickable commitment is ink-blue. Status colors never stand in for actions except the danger button.

## Typography

**Display Font:** Literata Variable (with Literata, PT Serif, Georgia, serif)
**Body Font:** Golos Text Variable (with system-ui, sans-serif)

**Character:** Literata gives titles and abstracts the feel of a printed paper with a Cyrillic-complete, readable face; Golos Text keeps controls and facts neutral and clear. Both ship through Fontsource.

### Hierarchy
- **Display** (600, clamp 1.875rem to 2.625rem, 1.15, -0.02em): page titles (h1).
- **Headline** (600, 1.625rem, 1.22): section titles (h2).
- **Title** (600, 1.3125rem, 1.28): sub-sections and modal titles (h3). Vacancy entry titles use a fluid 1.25rem to 1.5rem at 1.25.
- **Abstract** (serif 400, 1.125rem desktop and 1rem on phone, 1.6, 68ch max, clamped to 3 lines): vacancy abstracts and the page lead (muted ink-soft for leads).
- **Body** (sans 400, 1rem, 1.55): running interface text.
- **Label** (sans 600, 0.875rem, 1.35): field labels, active tabs and nav. Facts, org lines and hints are 0.875rem; tags and chips use 0.8125rem to 0.875rem. Sentence case throughout.

### Named Rules
**The Read-or-Operate Rule.** If a person reads it as prose, set it in Literata; if they click, scan or fill it, set it in Golos Text.
**The Tabular Numbers Rule.** Dates, counts and columns use tabular lining numerals (`.num`, deadline remainder).

## Layout

Single centered column, max 72rem, with a fluid gutter (1rem to 3.5rem, 4vw). Narrow pages use 44rem. Spacing runs on a 4px-based scale (4, 8, 12, 16, 24, 32, 48, 72) with `s-7` as page top padding (`s-6` on phones) and `s-8` above the footer. Controls are 44px tall (36px small); the search bar is 48px.

A vacancy entry is a two-column grid: title, organization, abstract, facts on the left; deadline on the right in an 11rem column, right-aligned. Under 40rem it collapses to one column with the deadline directly after the organization line. The header collapses at 52rem into a menu button and a stacked menu; role tabs go full width inside it. The search bar stacks vertically under 40rem. Toasts and modals rearrange under 30rem (toasts move to the top, modals become bottom sheets).

## Elevation & Depth

Flat by default. Structure comes from 1px hairlines, one black rule under the results count, and the warm Wash fill on the footer. A single shadow token (`0 1px 2px rgb(31 29 26 / 0.08), 0 12px 32px -8px rgb(31 29 26 / 0.22)`) belongs to floating layers only: modal, toast, combobox list. Focus is a 2px ink-blue ring with offset; fields use a white inner gap plus the blue ring.

### Named Rules
**The Hairline Instead of Card Rule.** Group content with a rule and spacing, not a box with a shadow.
**The Floating-Only Shadow Rule.** A shadow means "this layer sits above the page and will go away".

## Shapes

Square-ish and quiet: 6px for tags, option rows and skeletons; 10px for buttons, fields, modal and toasts; full pills only for filter chips. Bottom sheets round only the top corners. Active state in navigation is a flat 3px underline, not a pill or a side bar. Icons are inline SVG drawn in currentColor.

## Components

### Buttons
- **Shape:** 10px corners, 44px minimum height (36px small), semibold.
- **Primary:** ink-blue fill, white text, 8px 20px padding; hover deepens to ink-blue-deep; active nudges 1px down.
- **Secondary:** white with an edge border, wash fill on hover. **Quiet:** transparent, blue text, blue-wash hover. **Danger:** fail fill, deepens on hover.
- **Disabled:** wash-deep fill, soft text, not-allowed cursor. **Loading:** inline 16px spinner, progress cursor.
- **Focus:** 2px ink-blue outline, 2px offset.

### Chips and Tags
- **Chip:** white pill with an edge border, 36px tall; hover adds ink border and wash fill; pressed state is blue-wash fill, blue border, blue semibold text.
- **Tag:** 6px corners, wash fill, line border, 0.8125rem; the accent tag is blue-wash with blue text.

### Inputs / Fields
- **Style:** 1px edge border, white fill, 10px corners, 44px height, label above in semibold 0.875rem, hint and error below in 0.875rem.
- **Hover:** border turns ink. **Focus:** blue border with a double ring (2px white, 2px blue).
- **Error:** fail border, fail-wash fill, message in fail color. **Disabled:** wash fill, line border.
- Select is the native element with a drawn chevron; the combobox adds a floating white list (edge border, shadow) with blue-wash active row and a drawn check on the selected option.

### Navigation
Sticky white header with a hairline bottom rule, 4.25rem tall. Serif ink-blue wordmark (1.625rem, 700), sans links in ink-soft that turn ink on hover. The current page is ink, semibold, with a 3px marker underline. The role switcher ("Ищу работу" / "Нанимаю") is two text tabs with the same marker underline on the active one. In the mobile menu the active link carries an inset 3px marker line over the hairline.

### Vacancy Entry (signature)
Serif title (ink, underlined in blue on hover), organization line in ink-soft medium, a 3-line serif abstract, a facts row of 0.875rem ink-soft items with ink semibold values, and the deadline column at the right. Entries separate by a 1px line.

### Deadline (signature)
Date in 0.875rem. Only an urgent deadline gets the marker: a yellow band under the lower 45% of the date, drawn once in 700ms on appear. Normal deadlines are medium weight ink; expired ones are ink-soft. The remaining-time text sits beneath in ink-soft tabular numerals.

### Results Header
Count line in 0.875rem ink-soft above a 1px solid ink rule.

### Modal and Toast
Modal: white, 10px corners, shadow, scrim of ink at 50%, header with serif title and close button, hairline above actions. Toast: ink background, white text, 10px corners, shadow; the icon carries the kind (green, red, or yellow for info). Both enter with a 240ms ease-out rise.

### Skeleton and Empty State
Skeleton blocks sweep a wash to wash-deep shimmer over 1.6s and mirror the entry layout. Empty state is centered, 30rem wide, with an edge-colored (or fail for errors) illustration, text in ink-soft, and actions below.

## Do's and Don'ts

### Do:
- **Do** use ink-blue (#1c2c66) for every primary action and link, and keep marker yellow (#f7d559) for urgent deadlines and the current place in navigation.
- **Do** separate list entries with a 1px line (#e4e1dc) and give the results count a single ink rule.
- **Do** set titles, abstracts and leads in Literata and everything operable in Golos Text.
- **Do** keep control borders at edge (#85807a) so fields stay above 3:1 on white, and keep every control at least 44px tall on touch layouts.
- **Do** use tokens from `tokens.css` only; components do not introduce their own colors, sizes or spacing.
- **Do** make the deadline visible before the abstract on a phone.
- **Do** respect reduced motion; animations drop to near zero duration.

### Don't:
- **Don't** build a card grid with logo tiles, boxed shadows and a blue button.
- **Don't** add shadows to anything that sits in the page flow; shadows are for modal, toast and combobox list.
- **Don't** use yellow as a button fill, panel background or decoration, and don't highlight non-urgent dates.
- **Don't** add a kicker or eyebrow above headings, colored side borders, or gradient text.
- **Don't** add a dark theme or a second accent color without a new decision.
- **Don't** invent statistics or logos in demo content.
