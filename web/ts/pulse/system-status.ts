/**
 * System Status Section - Daemon control
 */

/**
 * Render System Status section
 */
export function renderSystemStatus(daemonStatus: any): string {
    const running = daemonStatus?.running || false;

    return `
        <div class="pulse-daemon-status">
            <span class="pulse-daemon-badge ${running ? 'running' : 'stopped'}">
                ${running ? '꩜ Running' : '꩜ Stopped'}
            </span>
            <button class="pulse-btn pulse-btn-sm" data-action="${running ? 'stop' : 'start'}-daemon">
                ${running ? 'Stop' : 'Start'}
            </button>
        </div>
    `;
}

/**
 * Handle system status actions (start/stop daemon, edit budget)
 */
export async function handleSystemStatusAction(action: string): Promise<void> {
    const { sendMessage } = await import('../websocket.ts');

    switch (action) {
        case 'start-daemon':
        case 'stop-daemon':
            const daemonAction = action === 'start-daemon' ? 'start' : 'stop';
            sendMessage({
                type: 'daemon_control',
                action: daemonAction
            });
            break;

        case 'edit-budget':
            openBudgetConfigPanel();
            break;
    }
}

/**
 * Open the budget configuration panel and populate current values
 */
function openBudgetConfigPanel(): void {
    const overlay = document.getElementById('pulse-config-overlay');
    const form = document.getElementById('pulse-config-form') as HTMLFormElement;
    const dailyInput = document.getElementById('daily-budget') as HTMLInputElement;
    const weeklyInput = document.getElementById('weekly-budget') as HTMLInputElement;
    const monthlyInput = document.getElementById('monthly-budget') as HTMLInputElement;
    const closeBtn = document.getElementById('pulse-config-close');

    if (!overlay || !form || !dailyInput || !weeklyInput || !monthlyInput) {
        console.error('Budget config panel elements not found');
        return;
    }

    // Fetch current config from server
    fetch('/api/pulse/config')
        .then(res => res.json())
        .then(config => {
            dailyInput.value = (config.daily_budget_usd ?? 1.0).toString();
            weeklyInput.value = (config.weekly_budget_usd ?? 7.0).toString();
            monthlyInput.value = (config.monthly_budget_usd ?? 30.0).toString();
        })
        .catch(err => {
            console.error('Failed to fetch pulse config:', err);
            // Use defaults
            dailyInput.value = '1.0';
            weeklyInput.value = '7.0';
            monthlyInput.value = '30.0';
        });

    // Show overlay
    overlay.style.display = 'flex';

    // Handle close
    const closeHandler = () => {
        overlay.style.display = 'none';
    };

    closeBtn?.addEventListener('click', closeHandler, { once: true });
    overlay.addEventListener('click', (e) => {
        if (e.target === overlay) closeHandler();
    }, { once: true });

    // Handle form submit
    form.onsubmit = async (e) => {
        e.preventDefault();
        const { sendMessage } = await import('../websocket.ts');

        sendMessage({
            type: 'pulse_config_update',
            daily_budget: parseFloat(dailyInput.value) || 1.0,
            weekly_budget: parseFloat(weeklyInput.value) || 7.0,
            monthly_budget: parseFloat(monthlyInput.value) || 30.0
        });

        overlay.style.display = 'none';
    };
}
