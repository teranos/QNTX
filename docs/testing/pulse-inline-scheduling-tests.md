# Test Plan: Pulse Inline Scheduling (Variation 1)

Comprehensive test cases for inline scheduling controls on ATS code blocks.

## Test Environment Setup

### Prerequisites
- QNTX server running (`make dev`)
- Database with `scheduled_pulse_jobs` table
- ProseMirror editor with pulse integration
- Browser with DevTools for debugging

### Test Data Setup

```sql
-- Ensure pulse tables exist
SELECT name FROM sqlite_master WHERE type='table' AND name='scheduled_pulse_jobs';

-- Clear existing test jobs
DELETE FROM scheduled_pulse_jobs WHERE ats_code LIKE '%test%';
```

---

## Unit Tests

### 1. Type System Tests

**File**: `web/ts/pulse/types.ts`

#### Test Case 1.1: Interval Formatting
```typescript
import { formatInterval } from './types.ts';

// Test cases
expect(formatInterval(30)).toBe('30s');
expect(formatInterval(60)).toBe('1m');
expect(formatInterval(90)).toBe('1m');      // Rounds down
expect(formatInterval(3600)).toBe('1h');
expect(formatInterval(7200)).toBe('2h');
expect(formatInterval(86400)).toBe('1d');
expect(formatInterval(172800)).toBe('2d');
```

**Expected**: All intervals format correctly to human-readable strings

#### Test Case 1.2: Interval Parsing
```typescript
import { parseInterval } from './types.ts';

// Valid inputs
expect(parseInterval('30s')).toBe(30);
expect(parseInterval('5m')).toBe(300);
expect(parseInterval('2h')).toBe(7200);
expect(parseInterval('1d')).toBe(86400);

// Invalid inputs
expect(parseInterval('invalid')).toBeNull();
expect(parseInterval('30')).toBeNull();      // Missing unit
expect(parseInterval('s')).toBeNull();       // Missing value
expect(parseInterval('-5m')).toBeNull();     // Negative
```

**Expected**: Valid formats parse correctly, invalid formats return null

---

### 2. API Client Tests

**File**: `web/ts/pulse/api.ts`

#### Test Case 2.1: List Scheduled Jobs
```typescript
import { listScheduledJobs } from './api.ts';

// Setup: Create test jobs via API
// Test
const jobs = await listScheduledJobs();

// Assertions
expect(Array.isArray(jobs)).toBe(true);
expect(jobs.length).toBeGreaterThanOrEqual(0);
if (jobs.length > 0) {
  expect(jobs[0]).toHaveProperty('id');
  expect(jobs[0]).toHaveProperty('ats_code');
  expect(jobs[0]).toHaveProperty('state');
}
```

**Expected**: Returns array of ScheduledJob objects

#### Test Case 2.2: Create Scheduled Job
```typescript
import { createScheduledJob } from './api.ts';

const request = {
  ats_code: 'ix https://example.com/test-jobs',
  interval_seconds: 3600,
  created_from_doc: 'test-doc-123',
};

const job = await createScheduledJob(request);

// Assertions
expect(job.id).toBeTruthy();
expect(job.ats_code).toBe(request.ats_code);
expect(job.interval_seconds).toBe(request.interval_seconds);
expect(job.state).toBe('active');
expect(job.next_run_at).toBeTruthy();
```

**Expected**: Returns created job with server-generated ID and timestamps

#### Test Case 2.3: Pause/Resume Job
```typescript
import { createScheduledJob, pauseScheduledJob, resumeScheduledJob } from './api.ts';

// Create job
const job = await createScheduledJob({
  ats_code: 'ix https://example.com/test',
  interval_seconds: 3600,
});

// Pause
const pausedJob = await pauseScheduledJob(job.id);
expect(pausedJob.state).toBe('paused');

// Resume
const resumedJob = await resumeScheduledJob(job.id);
expect(resumedJob.state).toBe('active');
```

**Expected**: State transitions work correctly

#### Test Case 2.4: Delete Job
```typescript
import { createScheduledJob, deleteScheduledJob, getScheduledJob } from './api.ts';

const job = await createScheduledJob({
  ats_code: 'ix https://example.com/test',
  interval_seconds: 3600,
});

await deleteScheduledJob(job.id);

// Verify deleted (should be inactive state)
const deletedJob = await getScheduledJob(job.id);
expect(deletedJob.state).toBe('inactive');
```

**Expected**: Job is set to inactive state (soft delete)

#### Test Case 2.5: Error Handling
```typescript
import { getScheduledJob, updateScheduledJob } from './api.ts';

// Non-existent job
try {
  await getScheduledJob('nonexistent-id');
  fail('Should have thrown error');
} catch (error) {
  expect(error.message).toContain('not found');
}

// Invalid update
try {
  await updateScheduledJob('nonexistent-id', { state: 'active' });
  fail('Should have thrown error');
} catch (error) {
  expect(error.message).toBeTruthy();
}
```

**Expected**: API errors are properly caught and surfaced

---

### 3. Scheduling Controls Component Tests

**File**: `web/ts/pulse/scheduling-controls.ts`

#### Test Case 3.1: Render Add Schedule Button
```typescript
import { createSchedulingControls } from './scheduling-controls.ts';

const container = createSchedulingControls({
  atsCode: 'ix https://example.com/test',
});

// Assertions
expect(container.querySelector('.pulse-btn-add-schedule')).toBeTruthy();
expect(container.querySelector('.pulse-icon')?.textContent).toBe('꩜');
expect(container.textContent).toContain('Add Schedule');
```

**Expected**: Renders "Add Schedule" button with pulse icon

#### Test Case 3.2: Render Existing Job Controls
```typescript
import { createSchedulingControls } from './scheduling-controls.ts';

const existingJob = {
  id: 'test-job-123',
  ats_code: 'ix https://example.com/test',
  interval_seconds: 3600,
  state: 'active',
  // ... other required fields
};

const container = createSchedulingControls({
  atsCode: existingJob.ats_code,
  existingJob,
});

// Assertions
expect(container.querySelector('.pulse-schedule-badge')).toBeTruthy();
expect(container.querySelector('.pulse-interval')?.textContent).toBe('1h');
expect(container.querySelector('.pulse-state')?.textContent).toBe('active');
expect(container.querySelector('.pulse-btn-pause')).toBeTruthy();
expect(container.querySelector('.pulse-interval-select')).toBeTruthy();
```

**Expected**: Renders badge and controls for existing job

#### Test Case 3.3: Interval Selection Interaction
```typescript
import { createSchedulingControls } from './scheduling-controls.ts';

let createdJob = null;

const container = createSchedulingControls({
  atsCode: 'ix https://example.com/test',
  onJobCreated: (job) => { createdJob = job; },
});

// Click "Add Schedule"
const addBtn = container.querySelector('.pulse-btn-add-schedule');
addBtn.click();

// Verify interval picker appears
expect(container.querySelector('.pulse-interval-picker')).toBeTruthy();
expect(container.querySelector('.pulse-interval-select')).toBeTruthy();

// Select interval
const select = container.querySelector('.pulse-interval-select');
select.value = '3600'; // 1 hour
select.dispatchEvent(new Event('change'));

// Click confirm
const confirmBtn = container.querySelector('.pulse-btn-confirm');
confirmBtn.click();

// Wait for API call
await new Promise(resolve => setTimeout(resolve, 100));

// Assertions
expect(createdJob).toBeTruthy();
expect(createdJob.interval_seconds).toBe(3600);
```

**Expected**: User can select interval and create job

---

## Integration Tests

### 4. ProseMirror Node View Tests

**File**: `web/ts/pulse/ats-node-view.ts`

#### Test Case 4.1: Node View Renders Correctly
```typescript
import { Schema } from 'prosemirror-model';
import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { createATSNodeViewFactory } from './ats-node-view.ts';

// Create test schema with code_block node
const schema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    code_block: {
      attrs: { scheduledJobId: { default: null } },
      content: 'text*',
    },
    text: {},
  },
});

// Create editor with ATS node view
const state = EditorState.create({
  schema,
  doc: schema.node('doc', null, [
    schema.node('code_block', null, [schema.text('ix https://example.com/test')]),
  ]),
});

const view = new EditorView(document.createElement('div'), {
  state,
  nodeViews: {
    code_block: createATSNodeViewFactory(),
  },
});

// Assertions
const codeBlock = view.dom.querySelector('.ats-code-block-wrapper');
expect(codeBlock).toBeTruthy();
expect(codeBlock.querySelector('.pulse-scheduling-controls')).toBeTruthy();
expect(codeBlock.querySelector('.pulse-btn-add-schedule')).toBeTruthy();
```

**Expected**: Node view renders with scheduling controls

#### Test Case 4.2: Schedule Creation Updates Node Attributes
```typescript
// ... setup editor as above

const addBtn = view.dom.querySelector('.pulse-btn-add-schedule');
addBtn.click();

// Select interval and confirm
const select = view.dom.querySelector('.pulse-interval-select');
select.value = '3600';
const confirmBtn = view.dom.querySelector('.pulse-btn-confirm');
confirmBtn.click();

// Wait for API call
await new Promise(resolve => setTimeout(resolve, 500));

// Check that node attributes were updated
const nodePos = 0;
const node = view.state.doc.nodeAt(nodePos);
expect(node.attrs.scheduledJobId).toBeTruthy();
expect(node.attrs.scheduledJobId).toMatch(/^SP/); // ASID format
```

**Expected**: Creating schedule updates ProseMirror document attributes

#### Test Case 4.3: Schedule Deletion Removes Attribute
```typescript
// ... setup editor with existing scheduled job

const deleteBtn = view.dom.querySelector('.pulse-btn-delete');

// Mock confirm dialog
window.confirm = () => true;

deleteBtn.click();

// Wait for API call
await new Promise(resolve => setTimeout(resolve, 500));

// Check that node attribute was removed
const nodePos = 0;
const node = view.state.doc.nodeAt(nodePos);
expect(node.attrs.scheduledJobId).toBeNull();

// Check that UI reverted to "Add Schedule"
expect(view.dom.querySelector('.pulse-btn-add-schedule')).toBeTruthy();
```

**Expected**: Deleting schedule removes attribute and reverts UI

---

## End-to-End Tests

### 5. Complete User Workflows

#### Test Case 5.1: Create Schedule from Scratch
**Steps**:
1. Open editor with new ATS code block
2. Type: `ix https://example.com/careers`
3. Verify "Add Schedule" button appears
4. Click "Add Schedule"
5. Select "6 hours" from dropdown
6. Click confirm (✓)
7. Verify badge appears: `꩜ 6h active`
8. Verify pause button (⏸) is present

**Expected**: User can create scheduled job through UI

#### Test Case 5.2: Pause and Resume Job
**Steps**:
1. Create scheduled job (as above)
2. Click pause button (⏸)
3. Verify badge changes to: `꩜ 6h paused`
4. Verify button changes to play (▶)
5. Click play button (▶)
6. Verify badge changes to: `꩜ 6h active`
7. Verify button changes to pause (⏸)

**Expected**: User can toggle job state

#### Test Case 5.3: Change Interval
**Steps**:
1. Create scheduled job with 6 hours
2. Click interval dropdown
3. Select "12 hours"
4. Verify badge updates to: `꩜ 12h active`
5. Verify backend job updated (check via API or database)

**Expected**: User can change interval through UI

#### Test Case 5.4: Delete Schedule
**Steps**:
1. Create scheduled job
2. Click delete button (🗑)
3. Confirm deletion dialog
4. Verify badge disappears
5. Verify "Add Schedule" button reappears
6. Verify job is inactive in backend (not hard-deleted)

**Expected**: User can remove schedule, UI reverts cleanly

#### Test Case 5.5: Document Persistence
**Steps**:
1. Create scheduled job on ATS block
2. Save document (trigger save mechanism)
3. Reload page / reopen document
4. Verify scheduled job badge reappears
5. Verify controls work (pause, delete, etc.)

**Expected**: Schedule persists across document saves/loads

---

## Visual Regression Tests

### 6. UI Appearance Tests

#### Test Case 6.1: Light Mode Styling
**Steps**:
1. Set browser to light mode
2. Create ATS block with active schedule
3. Take screenshot

**Expected Visual Elements**:
- Badge background: Light green (`#e8f5e9`)
- Badge text: Dark green (`#2e7d32`)
- Pulse icon (꩜) visible and slightly transparent
- Buttons have subtle borders
- Hover states work (lighter background)

#### Test Case 6.2: Dark Mode Styling
**Steps**:
1. Set browser to dark mode
2. Create ATS block with active schedule
3. Take screenshot

**Expected Visual Elements**:
- Badge background: Dark green (`#1b5e20`)
- Badge text: Light green (`#a5d6a7`)
- Controls have dark backgrounds
- Text is light colored
- Borders are visible but subtle

#### Test Case 6.3: State Badge Colors
**Steps**:
1. Create job in "active" state → Verify green badge
2. Pause job → Verify gray badge
3. Create job and delete → Verify red badge (if transitioning through inactive)

**Expected**: Badge colors reflect state correctly

#### Test Case 6.4: Pulse Animation
**Steps**:
1. Create active scheduled job
2. Observe pulse icon (꩜) for 5 seconds

**Expected**: Icon gently pulses (fades in/out over 2 second cycle)

---

## Performance Tests

### 7. Performance and Scalability

#### Test Case 7.1: Multiple Scheduled Blocks
**Steps**:
1. Create document with 10 ATS code blocks
2. Add schedules to all 10 blocks
3. Measure page load time
4. Measure interaction responsiveness

**Expected**:
- Page loads in < 2 seconds
- UI interactions respond in < 100ms
- No memory leaks after repeated interactions

#### Test Case 7.2: Rapid State Changes
**Steps**:
1. Create scheduled job
2. Rapidly click pause/resume 10 times
3. Verify final state is correct
4. Check for race conditions in API calls

**Expected**:
- No UI glitches
- Final state matches last user action
- No duplicate API calls
- Error handling graceful if conflicts occur

---

## Error Handling Tests

### 8. Error Scenarios

#### Test Case 8.1: Network Failure
**Steps**:
1. Disconnect network
2. Try to create scheduled job
3. Verify error message appears
4. Reconnect network
5. Retry operation

**Expected**: User sees friendly error, can retry successfully

#### Test Case 8.2: Invalid ATS Code
**Steps**:
1. Create ATS block with empty content
2. Try to add schedule
3. Verify validation error

**Expected**: Backend validation prevents creation, error shown to user

#### Test Case 8.3: Concurrent Modifications
**Steps**:
1. Open same document in two browser tabs
2. Tab 1: Create schedule on block A
3. Tab 2: Create different schedule on same block A
4. Verify conflict resolution

**Expected**: Last write wins, or conflict detected and user notified

#### Test Case 8.4: Deleted Job Externally
**Steps**:
1. Create scheduled job in UI
2. Delete job via API/CLI (external to editor)
3. Try to interact with scheduling controls in UI
4. Verify graceful error handling

**Expected**: UI detects job no longer exists, allows recreation

---

## Accessibility Tests

### 9. Accessibility Compliance

#### Test Case 9.1: Keyboard Navigation
**Steps**:
1. Use Tab key to navigate to scheduling controls
2. Use Space/Enter to activate buttons
3. Use arrow keys in dropdown

**Expected**: All controls accessible via keyboard

#### Test Case 9.2: Screen Reader Support
**Steps**:
1. Enable screen reader
2. Navigate to ATS block with schedule
3. Listen to announcements

**Expected**:
- Buttons have clear labels
- State changes announced
- Error messages read aloud

#### Test Case 9.3: Focus Indicators
**Steps**:
1. Tab through scheduling controls
2. Verify focus rings visible

**Expected**: Clear focus indicators on all interactive elements

---

## Browser Compatibility Tests

### 10. Cross-Browser Testing

Test all functionality in:
- ✅ Chrome (latest)
- ✅ Firefox (latest)
- ✅ Safari (latest)
- ✅ Edge (latest)

**Key areas**:
- Badge rendering
- Dropdown functionality
- Click handlers
- CSS animations
- Fetch API calls

---

## Regression Tests

### 11. Prevent Known Issues

#### Test Case 11.1: Badge State Sync
**Issue**: Badge doesn't update after pause/resume
**Test**: Verify badge text and color change immediately

#### Test Case 11.2: Attribute Persistence
**Issue**: Node attributes lost on document edits
**Test**: Edit text near scheduled block, verify attributes persist

#### Test Case 11.3: Memory Leaks
**Issue**: Event listeners not cleaned up
**Test**: Create/delete 100 schedules, check memory usage

---

## Manual Testing Checklist

- [ ] Add schedule to new ATS block
- [ ] Add schedule to existing ATS block
- [ ] Pause scheduled job
- [ ] Resume scheduled job
- [ ] Change interval via dropdown
- [ ] Delete scheduled job
- [ ] Multiple schedules in one document
- [ ] Document save/load with schedules
- [ ] Light mode appearance
- [ ] Dark mode appearance
- [ ] Mobile responsive layout
- [ ] Keyboard navigation
- [ ] Error handling for network failures
- [ ] Concurrent edits in multiple tabs

---

## Test Automation Setup

### Jest Configuration

```javascript
// jest.config.js
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'jsdom',
  moduleFileExtensions: ['ts', 'tsx', 'js'],
  transform: {
    '^.+\\.tsx?$': 'ts-jest',
  },
  testMatch: ['**/__tests__/**/*.test.ts'],
  collectCoverageFrom: [
    'web/ts/pulse/**/*.ts',
    '!web/ts/pulse/**/*.d.ts',
  ],
};
```

### Example Test File Structure

```
web/ts/pulse/__tests__/
├── types.test.ts
├── api.test.ts
├── scheduling-controls.test.ts
├── ats-node-view.test.ts
└── integration.test.ts
```

---

## Success Criteria

All tests must pass with:
- ✅ 100% of unit tests passing
- ✅ 95%+ code coverage
- ✅ All E2E workflows complete successfully
- ✅ No console errors during normal operation
- ✅ Accessibility score 90+ (Lighthouse)
- ✅ Visual regression tests within 5% pixel difference
- ✅ Performance budget: < 2s page load, < 100ms interactions

---

## Next Steps After Testing

1. Fix any discovered bugs
2. Add automated test suite
3. Run performance profiling
4. Conduct user acceptance testing
5. Document known limitations
6. Plan Variation 3 (Modal + Badge) evolution
