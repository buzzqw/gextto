# Accessibility analysis of the Gextto web interface

**Analysis date:** 29 September 2026  
**Scope:** the web interface in `uiweb/templates/`, `uiweb/static/` and the
Playwright tests in `uiweb/end2end/`  
**Reference:** WCAG 2.1 AA, with practices compatible with WCAG 2.2

## 1. Current status

Gextto has a substantially improved accessibility baseline, but it must still
be described as **partially accessible**. The automated checks pass, yet they do
not constitute a WCAG, EN 301 549 or legal-compliance certification.

A formal conclusion requires manual testing with keyboards, screen readers,
magnification and other assistive technologies on the supported browsers and
devices.

## 2. Assessment method

The current assessment combines:

- static review of HTML templates, CSS and client-side JavaScript;
- review of semantic HTML, accessible names, ARIA and focus management;
- review of responsive layouts, visible focus and reduced-motion handling;
- review of light and dark theme colors;
- automated axe-core checks through Playwright;
- regression tests for dialogs, keyboard sorting, polling and narrow viewports.

The automated suite currently contains **12 passing tests** and scans the main
views:

```text
/                         dashboard
/?view=downloads          downloads
/?view=settings           settings
/?view=maintenance        maintenance
/?view=health             health
/?view=logs               logs
```

Run it from a source checkout:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

The suite is a regression safety net, not a substitute for manual evaluation.

## 3. Implemented improvements

### 3.1 Names, language and landmarks

- The document exposes the selected language through the `lang` attribute.
- The shell provides skip navigation, `aside`, `nav`, `header` and `main`
  landmarks.
- Current navigation items expose `aria-current="page"`.
- Search, settings, language and dynamically generated controls have accessible
  names instead of relying only on placeholders or tooltips.
- Icon-only controls expose labels and retain visible supplementary tooltips.

### 3.2 Keyboard and focus behavior

- Dialogs move focus inside when opened, keep `Tab` and `Shift+Tab` within the
  active dialog, close with `Escape` and restore focus to the opener.
- Sortable table headers are keyboard-operable and expose their current
  `aria-sort` state.
- Focus is preserved when torrent data is refreshed by polling or when dynamic
  content is replaced.
- Links, buttons, form fields and navigation controls retain visible focus
  indicators.

### 3.3 Semantics and dynamic content

- Tables expose captions and column scope, including dynamically rendered
  tables.
- Progress indicators expose progress-bar semantics and their current value.
- Tabs, dialogs and landmarks expose their relevant ARIA state.
- Status messages, errors, notifications, progress and important asynchronous
  updates use suitable live regions.
- Decorative symbols do not replace the textual meaning of a control.
- Settings search includes common synonyms and abbreviations such as `memo`,
  `memory`, `RAM` and `cache`, improving discoverability for users who do not
  know the exact visible label.

### 3.4 Visual and responsive behavior

- Light-theme text and status colors have been darkened where necessary to
  improve contrast.
- The interface reflows at narrow widths and remains usable at mobile sizes;
  tables scroll within their panels instead of forcing the whole page to scroll.
- Reduced-motion preferences are respected.

## 4. Remaining validation work

The following work remains before making any formal accessibility statement:

1. Test all primary workflows manually with `Tab`, `Shift+Tab`, `Enter`,
   `Space`, arrow keys and `Escape`.
2. Test with at least one desktop screen reader, such as NVDA with Firefox or
   VoiceOver with Safari; test TalkBack on a supported Android device where
   applicable.
3. Test zoom and text resizing at 200% and 400%, including horizontal overflow,
   dialogs, tables and forms.
4. Verify every form's error summary, field association, invalid state and
   recovery path with a screen reader.
5. Check all important asynchronous states without relying on color, including
   loading, success, warning, failure, empty and retry states.
6. Repeat the checks in both themes, at mobile viewport sizes and with reduced
   motion enabled.
7. Review browser-specific behavior and any integrations that inject external
   content.

## 5. Acceptance criteria

The interface can be reconsidered for a formal AA assessment when:

- every interactive control has a reliable accessible name and role;
- every mouse action is available from the keyboard;
- dialogs contain focus correctly and restore it predictably;
- important asynchronous changes are announced without excessive interruption;
- text and meaningful graphics meet the applicable contrast requirements in
  both themes;
- tables, tabs, progress indicators and errors expose usable semantics;
- automated and manual tests show no regressions on desktop and mobile views.

## Conclusion

The current Gextto web interface is **partially accessible with a strong
automated baseline**, but no regulatory or legal conformance claim should be
made yet. The next required step is a documented manual audit with real
assistive technology, followed by an assessment against the requirements that
apply to the deployment.
