/**
 * QNTX's elements in a real browser: the specimens page (serve.ts), on a phone
 * and on a desktop. The test DOMs lay nothing out, so what is wider than its
 * screen is measured here and nowhere else.
 */

import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
    testDir: '.',
    testMatch: '*.browser.ts',
    outputDir: './test-results',
    fullyParallel: true,
    // Four WebKits on an 8GB laptop starve the page of its 30s to stand; two do not.
    workers: 2,
    forbidOnly: !!process.env.CI,
    retries: 0,
    reporter: process.env.CI ? [['list'], ['html', { open: 'never', outputFolder: './playwright-report' }]] : 'list',
    use: {
        baseURL: 'http://localhost:5181',
        // What the browser saw, kept when a test fails.
        trace: 'retain-on-failure',
    },
    projects: [
        { name: 'phone', use: { ...devices['Pixel 7'] } },
        // WebKit: the engine Chromium's emulation is not.
        { name: 'iphone', use: { ...devices['iPhone 15'] } },
        { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
        // "this doc is important for us"
        // WCAG 2.2, 1.4.10 Reflow: content at 320 CSS px, a desktop at 400%.
        { name: 'reflow', use: { ...devices['Desktop Chrome'], viewport: { width: 320, height: 640 } } },
    ],
    webServer: {
        command: 'bun browser/serve.ts',
        cwd: '..',
        url: 'http://localhost:5181',
        reuseExistingServer: !process.env.CI,
    },
});
