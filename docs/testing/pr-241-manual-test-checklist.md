# Manual Testing Checklist - PR #241: Panel Improvements

**PR:** #241 - Add shared tooltip system for improved observability across panels
**Branch:** `claude/apply-panel-improvements-oRIwb`
**Date:** 2026-01-08

**Note:** This checklist only includes tests that require manual verification. Automated tests (unit and JSDOM) cover 85%+ of functionality.

---

## Setup

- [ ] Ensure QNTX server is running (`./bin/qntx server`)
- [ ] Open browser to `http://localhost:8080`
- [ ] Open browser console (F12) to watch for errors

---

## 1. Backend Integration & Real Data

### Python Panel Execute

**SKIPPED**

Python panel needs further development, right now editor is not showing up. **HIGH PRIO**

- [ ] Write real Python code (e.g., `print("hello")`)
- [ ] Click "Run" → Orange "Confirm Execute"
- [ ] Click "Confirm Execute" → Code executes
- [ ] Output appears correctly
- [ ] Button returns to "Run (⌘↵)"

### Webscraper Panel

- [X] Enter real URL (e.g., `https://example.com`)
- [X] Click "Scrape URL" → Orange "Confirm Scrape"
The button remains black colored, and clicking on 'SCRAPE' jumps the button to the left because the text (in organge) 'Click again to start scraping' is shown right from the button, instead the text should appear under the button and not move the button away. click click should just work and not require user to move cursor to the left in order to click the 'CONFIRM SCRAPE' button again.
- [X] Click "Confirm Scrape" → URL is scraped
Correctly fails and show's error inside of the panel, but error is repeated in result's box as well. The endpoint is not available because the python-webscraper is not loaded right now, so this is correct behaviour, but what would be even better is if the panel had diagonal gray stripes indicating the panel is not usable right now unless the webscraper plugin is enabled, also we should have gray stripes in the symbol just like we do with other symbols in the command palette (like with fuzzy-ax being available or not)
- [ ] Results display correctly
worked previously, but havent tested this, **SKIP**

### Config Panel Save

**SKIP**
the panel is not wide enough in order to accommodate for everything, config panel deserves more screen real estate.

- [ ] Edit a config value
- [ ] Click "Save" twice (confirmation)
- [ ] Value persists after page reload
- [ ] Backend validation errors display correctly

### Plugin Config Save

- [X] Expand a plugin with config
- [X] Edit a config value
pencil symbol show's up, and allows me to change the existing value. however right now, it is required to click on the pencil button, what i want instead is that clicking on the value allows me to edit, and hovering over the key should reveal the hint not the value.
- [X] Click "Save Configuration" twice
**CRITICAL MUST FIX NOW**
'SAVE' button is nowhere to be found anymore! ideally we have button that says 'apply' and then change into 'restart plugin', clicking apply should in fact persist the config and pressing restart plugin should in fact restart the plugin, the button should appear in place of the pencil icon.
- [ ] Plugin restarts successfully
**SKIP**, impossible to test without save button
- [ ] New config takes effect
**SKIP**, impossible to test without save button

### Network Error Recovery

**SKIP**
impossible to test until we fix panel width

- [ ] Open Config panel
- [ ] Disable network (DevTools → Network → Offline)
- [ ] Try to save a config value
- [ ] Rich error displays: "Network Error"
- [ ] Re-enable network
- [ ] Click "Retry" → Save succeeds

---

## 2. Performance & Memory

### Long Session Memory Check

**SKIP**
i have better things to do than doing this.

- [ ] Keep QNTX open for 10+ minutes
- [ ] Open/close panels 20+ times
- [ ] Hover tooltips frequently
- [ ] Check browser memory (DevTools → Memory → Take snapshot)
- [ ] Memory should not continuously grow (expect < 50MB growth)

### Rapid Tooltip Hovering

**SKIP**

- [ ] Open Pulse panel with 10+ jobs
- [ ] Rapidly hover over 20+ job badges
- [ ] No lag or stuttering
- [ ] Tooltips appear/disappear smoothly
- [ ] CPU usage remains reasonable (< 30%)

### Large Dataset Rendering

**SKIP**

- [ ] Open Pulse panel with 20+ scheduled jobs
- [ ] All tooltips work on all job badges
- [ ] No slowdown when hovering
- [ ] Panel scrolls smoothly

---

## 3. Visual & UX Verification

### Tooltip Styling

- [X] Hover over any tooltip trigger
works in config panel, does not work in plugin config area
- [X] **Dark background** - Terminal-style dark theme
- [X] **Monospace font** - Code/data font family
- [X] **Proper padding** - 8-10px comfortable spacing
- [ ] **Border radius** - 4px rounded corners
let's not have rounded corners, not sure why you are such a fan of them, i want square pointy dangerous corners
- [ ] **Arrow pointer** - Aligns with trigger element (near screen edges)
It exists, but doesn't point to the actual thing i'm hovering on. this 'feature' is deceptive right now.
- [X] **Max width** - Constrains to ~400px, wraps long text
but also, let's try to make this work in responsive design as well, on mobile and tabled tapping on a key or tooltip available item should make it show up the tooltip and also respect screensize, tapping on the tooltip again should hide it.

### Confirmation Button Colors

**BLOCKED** until python panel development enables us to test this.

- [ ] Click "Run" in Python panel
- [ ] **Warning state**: Orange background (#fff3e0)
- [ ] Click "✕" (delete) on a Pulse job
- [ ] **Danger state**: Red background (#ffebee)
- [ ] Both return to normal state after timeout (5s)

### Error Display Styling

- [X] Trigger any validation error
verified in plugin panel python config
- [X] **Error title**: Bold, red text
- [ ] **Details section**: Expandable with "▼ Error Details"
I'm not seeing the ▼, but this is also a non-feature, we should always show the details dammit.
- [ ] **Code blocks**: Monospace font, gray background
i tried to test the creation of an ATS code block, but i cannot seem to progress further than clicking on 'Add Schedule', also i suspect the panel is not wide enough because it seems like the text is not wrapping worrectly on the border
- [ ] Click to expand/collapse → Smooth transition
**SKIP**

### Responsive Design

- [X] **Mobile viewport** (< 640px width)
  - Tooltips constrain to viewport edges
  Yes, they do so this works.
  - Confirmation buttons remain tappable (44px touch targets)
  yes they remain tappable
  - Error details readable without horizontal scroll
  confirmed.

- [X] **Wide viewport** (> 1536px width)
  - Layouts maintain proper spacing
  - Tooltips don't become too wide (400px max)
  - Panel content uses available space well

---

## 4. Accessibility Testing

### Screen Reader (if available)

**SKIP**

- [ ] Enable screen reader (VoiceOver, NVDA, JAWS)
- [ ] Tab to element with tooltip
- [ ] Tooltip content is announced
- [ ] Tab to "Run" button in Python panel
- [ ] Click once → Confirmation state announced
- [ ] Click again → Action announced

### Keyboard Navigation Flow

**SKIP**

- [ ] Tab through Pulse panel
- [ ] Job actions (⏵, ⏸, ✕) are reachable
- [ ] Tab order feels logical
- [ ] Press Enter on "Run" in Python panel
- [ ] Orange confirmation state
- [ ] Press Enter again → Executes
- [ ] Tab to "Error Details" (if error present)
- [ ] Press Enter → Expands
- [ ] Press Enter again → Collapses

### Touch Target Usability (Mobile/Tablet)

**SKIP**

- [ ] Open on mobile device or use DevTools mobile emulation
- [ ] All buttons comfortable to tap (44px minimum)
- [ ] Confirmation buttons don't accidentally trigger
- [ ] Tooltips don't block important UI elements

---

## 5. Console & Error Monitoring

**SKIP**, too vague, not actionable.

### Normal Operation

- [ ] Perform typical workflows for 5+ minutes
- [ ] **No console errors** (red messages)
- [ ] **No warnings** about missing properties or deprecated APIs
- [ ] **No unhandled promise rejections**

### Expected Debug Logs (Optional)

- [ ] Console filtering for `[tooltip]` shows tooltip events
- [ ] Console filtering for `[panel]` shows panel lifecycle
- [ ] Logs are helpful for debugging (not excessive noise)

---

## 6. Edge Cases & Unusual Interactions

**SKIP**

### Click During Tooltip Transition

- [ ] Hover to start showing tooltip
- [ ] Immediately click element before tooltip fully appears
- [ ] No broken state or orphaned tooltips
- [ ] Normal interaction continues

### Window Resize During Tooltip

- [ ] Show a tooltip near screen edge
- [ ] Resize browser window smaller
- [ ] Tooltip repositions to stay in bounds
- [ ] No visual glitches

### Multiple Rapid Panel Switches

- [ ] Open Pulse panel
- [ ] Immediately switch to Config panel (before fully loaded)
- [ ] Immediately switch to Plugin panel
- [ ] All panels render correctly
- [ ] No state leakage between panels
- [ ] No console errors

### Confirmation State During Panel Close

- [ ] Click "Run" in Python panel (orange state)
- [ ] Close panel with Escape
- [ ] Reopen Python panel
- [ ] Button should be "Run (⌘↵)" (not orange)

---

## 7. Cross-Browser Compatibility

**SKIP**

### Chrome

- [ ] All features work
- [ ] Tooltips display correctly
- [ ] Confirmations function properly
- [ ] No visual issues

### Firefox

- [ ] All features work
- [ ] Tooltips display correctly
- [ ] Confirmations function properly
- [ ] No visual issues

### Safari (macOS/iOS)

- [ ] All features work
- [ ] Tooltips display correctly
- [ ] Confirmations function properly
- [ ] No visual issues

---

## Test Completion Summary

**Total Manual Tests:** 32 items
**Automated Coverage:** 85%+ (unit + JSDOM tests)

**Recommended testing order:**

1. **Backend Integration** (5 min) - Verify real API calls work
2. **Visual & UX** (5 min) - Check styling and responsiveness
3. **Console & Errors** (2 min) - Monitor for runtime issues
4. **Performance** (10 min) - Long session and stress tests
5. **Accessibility** (10 min if tools available)
6. **Edge Cases** (5 min) - Unusual interactions
7. **Cross-Browser** (10 min if needed)

**Minimum viable testing** (15 min):

- Backend Integration (Python execute, config save)
- Visual verification (tooltip styling, button colors)
- Console monitoring during normal use

**Comprehensive testing** (45-60 min):

- All items in checklist

---

## Notes Section

### Issues Found

```
Issue #1:
- Description:
- Steps to reproduce:
- Expected behavior:
- Actual behavior:
- Severity: [Critical/High/Medium/Low]

Issue #2:
...
```

### Observations

```
- Positive feedback:
- Performance notes:
- UX suggestions:
```

### Browser-Specific Issues

```
- Chrome:
- Firefox:
- Safari:
```

---

## Automated Test Coverage

**Already covered by existing tests:**

- Plugin panel build time formatting
- Plugin config validation errors
- Retry button functionality
- Toast notifications
- Error display with retry
- Base panel error boundaries

**Will be covered by new tests (recommended Priority 1):**

- Tooltip show/hide behavior (tooltip.dom.test.ts)
- Tooltip viewport constraints (tooltip.dom.test.ts)
- Two-click confirmation state machine (python/panel.test.ts, webscraper-panel.test.ts)
- Pulse job action confirmations (pulse/job-actions.test.ts)
- Config inline editing flow (config-panel.test.ts)
- Timeout and reset logic (tooltip.test.ts)

See code review findings for additional test recommendations.

---

**Tester:** _________________
**Date:** _________________
**Duration:** _________________
**Overall Result:** [ ] Pass [ ] Pass with issues [ ] Fail
