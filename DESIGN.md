---
name: ИОН
description: A science job board set like a serious journal, with one dark wine accent for deadlines and navigation.
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
  marker-wine: "#722F37"
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
  sm: "2px"
  md: "4px"
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
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink-blue}"
    rounded: "{rounded.sm}"
  chip:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
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

# Design System: ИОН

## Overview

**Creative North Star: "The Journal"**

A vacancy is set like an article in a serious science journal: serif title, organization line, abstract, a row of facts. The page is pure white, the ink is warm near-black, actions and links are one deep ink-blue, and the single accent is a dark wine marker for urgent deadlines and the current place in navigation. Entries are separated by hairline rules, not boxed cards. The world refuses the generic job-board card grid with logo tiles and a bright blue button.

Density is calm and readable: generous line height, a 68ch reading measure, a 72rem content column. Phone and desktop are equal; the same entry reflows so the deadline stays visible before the abstract on a phone. There is no dark theme. Depth is typographic and linear (rules, weight, whitespace), not shadowed.

**Key Characteristics:**
- Serif for what is read (titles, abstracts, lead paragraphs); sans for what is operated (interface, facts, forms).
- One action color (ink-blue) and one accent (marker wine), never mixed in roles.
- Hairlines (1px) between entries and sections; one black rule under the result count.
- Printed corners: 2px small, 4px controls and panels; no pills (only the avatar and the bell counter are round).
- Rules carry the hierarchy like a printed journal: a 3px double rule under the masthead, a 3px ink rule above every rubric (section heads, landing table of contents, tracks, footer), 1px ink under a heading, 1px line between entries.
- Russian typesetting: one- and two-letter words are bound to the next word and a dash to the word before it with a non-breaking space (`lib/typo`) in vacancy titles, abstracts and the landing headline.
- Shadow only on floating layers (modal, toast, combobox list) and as the focus ring.

## Colors

A white sheet, warm ink, one blue, one dark wine; status colors are muted and used only for state.

### Primary
- **Ink Blue** (#1c2c66): primary buttons, links, selected chip text, focus ring, caret. Hover deepens to **Ink Blue Deep** (#121e4d). **Blue Wash** (#eceefa) is its tint for the active combobox option and quiet-button hover; a selected chip is filled ink-blue.

### Secondary
- **Marker Wine** (#722F37): the only accent. It fills urgent deadline dates and text selections with white lettering, underlines the active nav item and active section tab (3px), fills the selected segment of the role switch with white lettering, and colors the info icon on a small white backing in dark toasts. Empty-state artwork uses a white text stroke over its wine marker. The approved graphic logo keeps its blue artwork and wine tassel.

### Neutral
- **Paper** (#ffffff): page, fields, modal, secondary buttons.
- **Wash** (#f6f4f1) and **Wash Deep** (#ece9e4): tags, hover fills, the selected privacy row, disabled controls, skeleton shimmer.
- **Ink** (#1f1d1a): headings, key facts, toast background. **Ink Body** (#38352f): abstracts. **Ink Soft** (#5d5a55): secondary lines, placeholders, labels' flags.
- **Line** (#e4e1dc): hairlines between entries and sections. **Edge** (#85807a): control borders, held at about 3.9:1 on white so fields are findable.
- **Status:** OK (#1e6b45) with OK Wash (#e6f3ec); Fail (#a3261b), Fail Deep (#7f1c13) and Fail Wash (#fbebe9) for errors and the danger button. On dark toasts: On Ink (#ffffff), On Ink Soft (#d9d6d1), with lighter green (#8fdcb2) and red (#ffb3aa) icon tints defined in tokens.

### Named Rules
**The One Highlighter Rule.** Marker wine marks what must be done soon, the current place in navigation and the current account mode (the selected role-switch segment). It is never a button fill or a background panel. The same accent appears in the logo tassel and empty-state marker.
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

**Landing (`/`).** One grid for the whole page: the first row holds the headline and search on the left and a table of contents (open vacancies by field and by position type, rows set like a journal contents page: name, dotted leader, count in ink semibold) on the right; below it, full width: fresh vacancies, two tracks (seeker, employer) on a shared subgrid so headings, steps and buttons align, and a definition list "Устроено под науку". Under 62rem it is one column and the table of contents moves below the fresh vacancies. The headline uses a larger token `--text-hero` (clamp 2.25rem to 3.125rem): both sentences at weight 600 in ink (the two sides of the site speak with one voice; no grey second line). The numbers are a single row of ink-blue links, not big figures. The one authored motion is the rise of headline, lead and search (640ms, staggered).

A vacancy entry is a two-column grid: title, organization, abstract, facts and specialties on the left; deadline on the right in a fixed 11rem column, right-aligned. Under 40rem it collapses to one column with the deadline directly after the organization line. Rows of "Мои вакансии" follow the same grid (deadline and the edit button in the right column).

**Action column (`.split`).** A page about one thing that asks for one decision (vacancy, candidate card) has the text on the left and a 17rem column on the right under a 1px ink rule: deadline, the primary action, quiet secondary actions stacked full width. The column is sticky (below the header) so the action stays in view while reading. Under 62rem it moves directly under the page header and its buttons go back into a row.

The header collapses at 72rem into a menu button and a stacked menu; the role switch goes full width at the top of it. The search bar stacks vertically under 40rem. Toasts and modals rearrange under 30rem (toasts move to the top, modals become bottom sheets).

## Elevation & Depth

Flat by default. Structure comes from rules, as in print: the double rule under the masthead, 3px ink rules above rubrics, a 2px ink rule under the results count, 1px hairlines between entries. A single shadow token (`0 1px 2px rgb(31 29 26 / 0.08), 0 12px 32px -8px rgb(31 29 26 / 0.22)`) belongs to floating layers only: modal, toast, combobox list. Focus is a 2px ink-blue ring with offset; fields use a white inner gap plus the blue ring.

### Named Rules
**The Hairline Instead of Card Rule.** Group content with a rule and spacing, not a box with a shadow.
**The Floating-Only Shadow Rule.** A shadow means "this layer sits above the page and will go away".

## Shapes

Printed and quiet: 2px for tags, chips, option rows and skeletons; 4px for buttons, fields, the role switch, modal and toasts. No pills. Bottom sheets round only the top corners. Active state in navigation is a flat 3px underline, not a pill or a side bar. Icons are inline SVG drawn in currentColor.

## Components

### Buttons
- **Shape:** 4px corners, 44px minimum height (36px small), semibold.
- **Primary:** ink-blue fill, white text, 8px 20px padding; hover deepens to ink-blue-deep; active nudges 1px down.
- **Secondary:** white with an edge border, wash fill on hover. **Quiet:** transparent, blue text, blue-wash hover. **Danger:** fail fill, deepens on hover.
- **Disabled:** wash-deep fill, soft text, not-allowed cursor. **Loading:** inline 16px spinner, progress cursor.
- **Focus:** 2px ink-blue outline, 2px offset.
- **Touch:** buttons, links and fields use `touch-action: manipulation`; the tap flash is a 12% ink-blue tint (`--tap`), not the browser grey.

### Chips and Tags
- **Chip:** white with an edge border and 2px corners, 36px tall; hover adds ink border and wash fill; pressed state is an ink-blue fill with white semibold text.
- **Tag:** 2px corners, wash fill, line border, 0.8125rem; the accent tag ("Конкурс", Q1–Q2) is an ink-blue outline with blue semibold text and no fill, like a rubric stamp.

### Inputs / Fields
- **Style:** 1px edge border, white fill, 4px corners, 44px height, label above in semibold 0.875rem, hint and error below in 0.875rem.
- **Hover:** border turns ink. **Focus:** blue border with a double ring (2px white, 2px blue).
- **Error:** fail border, fail-wash fill, message in fail color. **Disabled:** wash fill, line border.
- Select is the native element with a drawn chevron; the combobox adds a floating white list (edge border, shadow) with blue-wash active row and a drawn check on the selected option.

### Navigation
Sticky white header closed by a 3px double ink rule, like a journal masthead, 4.25rem tall. The shared `BrandLogo` renders the approved graphic «ИОН» logo in both header and footer. The header logo links to the home page, with accessible name «ИОН» and a title with the expanded product name; the footer logo remains an image in a paragraph. The image has explicit 1376 × 548 dimensions to reserve its aspect ratio. The artwork is blue with a wine tassel. It displays 112px wide (about 45px high); in the header at widths up to 26rem it shrinks to 80px (about 32px high). Navigation links remain sans in ink-soft and turn ink on hover. The current page is ink, semibold, with a 3px marker underline; the marker in the header means only "you are here". A guest sees only the open sections (Вакансии, Учёные, Организации); personal sections and the role switch appear after sign-in. The account button shows only the first name (the first word of the display name, or the whole name when that word is shorter than three letters); the full name and email open in its panel. The navigation never shrinks: when space runs out, the account button truncates, so nav items never touch the role switch. In the seeker role «Избранное» is not a menu item but a bookmark icon beside the bell (44px, filled with a 3px marker underline when the section is current; tooltip and accessible name «Избранное»); it stays in the bar on phones too. The role switch ("Ищу работу" / "Нанимаю") is a segmented control, not tabs: two segments in a 1px wine frame with 4px corners and a wine divider; the active segment is filled marker wine with white semibold text (D-254: the earlier black fill fell outside the palette). It differs from nav items by shape, a filled segment in a frame rather than an underline. Its inset keyboard focus outline is white; the inactive segment uses wine. In the mobile menu the active link carries an inset 3px marker line over the hairline.

### Vacancy Entry (signature)
Serif title (ink, underlined in blue on hover), organization line in ink-soft medium, a 3-line serif abstract, a facts row in 0.875rem ink-body joined by edge-colored middle dots (the accent "Конкурс" tag leads the row, the level code is ink semibold), the specialties on their own ink-soft line, and the deadline column at the right. A vacancy without a deadline says "Без срока подачи / приём до закрытия" in ink-soft, so the column never collapses and the bookmark stays at the same height. Entries separate by a 1px line. On the vacancy page VAK specialties are a list with the code in its own 3.5rem column, never wrapped.

### Deadline (signature)
Date in 0.875rem. Only an urgent deadline gets the marker: a full wine fill with white lettering, revealed together from left to right once in 480ms (150ms delay) on appear so text stays readable throughout; with reduced motion it is simply there. Normal deadlines are medium weight ink; expired ones are ink-soft. The remaining-time text sits beneath in ink-soft tabular numerals.

### Results Header
Count line in 0.875rem ink-soft above a 2px solid ink rule.

### Modal and Toast
Modal: white, 4px corners, shadow, scrim of ink at 50%, header with serif title and close button, hairline above actions. Toast: ink background, white text, 4px corners, shadow; the icon carries the kind (green, red, or wine on a small white backing for info). Both enter with a 240ms ease-out rise (the toast as a transition from `@starting-style`, so quick successive toasts retarget instead of restarting). With reduced motion both only fade.

### Skeleton and Empty State
Skeleton blocks sweep a wash to wash-deep shimmer over 1.6s and mirror the entry layout. Empty state is centered, 30rem wide, with an edge-colored (or fail for errors) illustration, text in ink-soft, and actions below.

## Do's and Don'ts

### Do:
- **Do** use ink-blue (#1c2c66) for every primary action and link, and keep marker wine (#722F37) for urgent deadlines, the current place in navigation and the selected account mode.
- **Do** separate list entries with a 1px line (#e4e1dc) and give the results count a single ink rule.
- **Do** set titles, abstracts and leads in Literata and everything operable in Golos Text.
- **Do** keep control borders at edge (#85807a) so fields stay above 3:1 on white, and keep every control at least 44px tall on touch layouts.
- **Do** use tokens from `tokens.css` only; components do not introduce their own colors, sizes or spacing.
- **Do** make the deadline visible before the abstract on a phone.
- **Do** respect reduced motion: anything that moves (rise, marker draw, skeleton sweep, chevron turn) appears in place or only fades; the button spinner keeps turning because it reports state.
- **Do** put the person or the thing first and its actions after: a profile shows name, headline and contacts, then buttons; a long vacancy repeats its apply button after the requirements.
- **Do** keep one ink-blue button per screen region; list rows get secondary or quiet actions and are opened by their title.
- **Do** give standalone links (number rows, "all vacancies", footer, organization line) an invisible vertical hit area of at least 24px, about 44px on touch screens.

### Don't:
- **Don't** build a card grid with logo tiles, boxed shadows and a blue button.
- **Don't** add shadows to anything that sits in the page flow; shadows are for modal, toast and combobox list.
- **Don't** use the wine accent as a button fill, panel background or decoration, and don't highlight non-urgent dates.
- **Don't** add a kicker or eyebrow above headings, colored side borders, or gradient text.
- **Don't** bring back pills, tinted option cards or a grey second half of a headline: they are what made the first rendition read as generated (D-132).
- **Don't** add a dark theme or a second accent color without a new decision.
- **Don't** invent statistics or logos in demo content.
- **Don't** advertise the service as free anywhere in the interface, emails or README (D-129).
