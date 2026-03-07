// Hex Viewer glyph module for ix-bin plugin.
//
// Renders a binary data inspector on the QNTX canvas. Supports:
// - Binary file upload/drag-drop
// - Server-side format detection and structure parsing
// - Hex dump with ASCII sidebar
// - Ingestion trigger (creates attestations in ATS)

export function render(glyph, ui) {
  const container = document.createElement('div');
  container.style.cssText = 'font-family: monospace; font-size: 13px; padding: 12px; display: flex; flex-direction: column; height: 100%; gap: 8px; overflow: hidden; color: #33ff33;';

  // Header
  const header = document.createElement('div');
  header.style.cssText = 'display: flex; align-items: center; gap: 8px; flex-shrink: 0;';
  header.innerHTML = '<span style="font-size: 16px; font-weight: 600;">Binary Inspector</span>';
  container.appendChild(header);

  // Status bar
  const status = document.createElement('div');
  status.style.cssText = 'font-size: 11px; color: #88cc88; flex-shrink: 0;';
  status.textContent = 'Drop or paste a binary file to inspect';
  container.appendChild(status);

  // Action bar
  const actions = document.createElement('div');
  actions.style.cssText = 'display: flex; gap: 6px; flex-shrink: 0;';

  const fileInput = document.createElement('input');
  fileInput.type = 'file';
  fileInput.style.cssText = 'font-size: 12px; font-family: monospace;';

  const ingestBtn = document.createElement('button');
  ingestBtn.textContent = 'Ingest';
  ingestBtn.disabled = true;
  ingestBtn.style.cssText = 'font-family: monospace; font-size: 12px; padding: 4px 12px; cursor: pointer; border: 1px solid #555; background: #222; color: #eee;';

  actions.appendChild(fileInput);
  actions.appendChild(ingestBtn);
  container.appendChild(actions);

  // Summary panel — populated by server /detect response
  const summary = document.createElement('div');
  summary.style.cssText = 'font-size: 12px; padding: 6px 8px; background: #111; border: 1px solid #333; flex-shrink: 0; display: none; white-space: pre-wrap; word-break: break-word; overflow-wrap: break-word;';
  container.appendChild(summary);

  // Hex view area
  const hexView = document.createElement('pre');
  hexView.style.cssText = 'flex: 1; overflow: auto; margin: 0; padding: 8px; background: #1a1a2e; border: 1px solid #333; font-size: 12px; line-height: 1.4; white-space: pre; word-break: break-word; overflow-wrap: break-word; color: #33ff33;';
  hexView.textContent = '';
  container.appendChild(hexView);

  let currentData = null;

  // Load file: render hex client-side, detect format server-side
  function loadFile(file) {
    const reader = new FileReader();
    reader.onload = function () {
      currentData = new Uint8Array(reader.result);
      status.textContent = file.name + ' \u2014 ' + currentData.length + ' bytes';
      ingestBtn.disabled = false;
      renderHex(currentData, hexView);
      detectFormat(currentData, summary);
    };
    reader.readAsArrayBuffer(file);
  }

  // File selection
  fileInput.addEventListener('change', function () {
    if (fileInput.files[0]) loadFile(fileInput.files[0]);
  });

  // Drag and drop
  container.addEventListener('dragover', function (e) { e.preventDefault(); });
  container.addEventListener('drop', function (e) {
    e.preventDefault();
    if (e.dataTransfer.files[0]) loadFile(e.dataTransfer.files[0]);
  });

  // Ingest button
  ingestBtn.addEventListener('click', async function () {
    if (!currentData) return;
    ingestBtn.disabled = true;
    ingestBtn.textContent = 'Ingesting...';
    try {
      const resp = await fetch('/api/ix-bin/ingest', {
        method: 'POST',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: currentData,
      });
      const text = await resp.text();
      if (!resp.ok) {
        status.textContent = 'Ingest failed: ' + resp.status + ' ' + text.slice(0, 200);
      } else {
        const result = JSON.parse(text);
        status.textContent = 'Ingested: ' + result.format + ' \u2014 ' + (result.attestations_created || 0) + ' attestations created';
        if (result.last_error) {
          status.textContent += ' (error: ' + result.last_error + ')';
        }
      }
    } catch (err) {
      status.textContent = 'Ingest failed: ' + err.message;
    }
    ingestBtn.textContent = 'Ingest';
    ingestBtn.disabled = false;
  });

  return container;
}

// Server-side format detection — single source of truth
async function detectFormat(data, element) {
  element.style.display = 'none';
  try {
    const resp = await fetch('/api/ix-bin/detect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: data,
    });
    if (!resp.ok) return;
    const result = await resp.json();

    // Build summary from server response
    let info = 'Format: ' + result.format;
    if (result.summary) {
      var keys = Object.keys(result.summary);
      for (var i = 0; i < keys.length; i++) {
        var k = keys[i];
        if (k !== 'format') {
          info += '\n' + k + ': ' + result.summary[k];
        }
      }
    }
    element.textContent = info;
    element.style.display = 'block';
  } catch (_) {
    // Silent — summary panel stays hidden
  }
}

// Render hex dump in the pre element (client-side, no server round-trip needed)
function renderHex(data, element) {
  const lines = [];
  const bytesPerLine = 16;
  const maxLines = Math.min(Math.ceil(data.length / bytesPerLine), 512);

  for (let i = 0; i < maxLines * bytesPerLine && i < data.length; i += bytesPerLine) {
    let offset = i.toString(16).padStart(8, '0');
    let hex = '';
    let ascii = '';

    for (let j = 0; j < bytesPerLine; j++) {
      if (i + j < data.length) {
        hex += data[i + j].toString(16).padStart(2, '0') + ' ';
        const b = data[i + j];
        ascii += (b >= 0x20 && b <= 0x7e) ? String.fromCharCode(b) : '.';
      } else {
        hex += '   ';
      }
      if (j === 7) hex += ' ';
    }

    lines.push(offset + '  ' + hex + ' |' + ascii + '|');
  }

  if (data.length > maxLines * bytesPerLine) {
    lines.push('... (' + (data.length - maxLines * bytesPerLine) + ' more bytes)');
  }

  element.textContent = lines.join('\n');
}
