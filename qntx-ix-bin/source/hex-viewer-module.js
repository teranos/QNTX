// Hex Viewer glyph module for ix-bin plugin.
//
// Renders a binary data inspector on the QNTX canvas. Supports:
// - Binary file upload/paste
// - Automatic format detection
// - Hex dump with ASCII sidebar
// - Parsed structure overlay for known formats (PCAP, ELF)
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

  // Summary panel
  const summary = document.createElement('div');
  summary.style.cssText = 'font-size: 12px; padding: 6px 8px; background: #111; border: 1px solid #333; flex-shrink: 0; display: none; white-space: pre-wrap; word-break: break-word; overflow-wrap: break-word;';
  container.appendChild(summary);

  // Hex view area
  const hexView = document.createElement('pre');
  hexView.style.cssText = 'flex: 1; overflow: auto; margin: 0; padding: 8px; background: #1a1a2e; border: 1px solid #333; font-size: 12px; line-height: 1.4; white-space: pre; word-break: break-word; overflow-wrap: break-word; color: #33ff33;';
  hexView.textContent = '';
  container.appendChild(hexView);

  let currentData = null;

  // File selection handler
  fileInput.addEventListener('change', function () {
    const file = fileInput.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = function () {
      currentData = new Uint8Array(reader.result);
      status.textContent = file.name + ' \u2014 ' + currentData.length + ' bytes';
      ingestBtn.disabled = false;
      renderHex(currentData, hexView);
      detectAndSummarize(currentData, summary);
    };
    reader.readAsArrayBuffer(file);
  });

  // Drag and drop
  container.addEventListener('dragover', function (e) { e.preventDefault(); });
  container.addEventListener('drop', function (e) {
    e.preventDefault();
    const file = e.dataTransfer.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = function () {
      currentData = new Uint8Array(reader.result);
      status.textContent = file.name + ' \u2014 ' + currentData.length + ' bytes';
      ingestBtn.disabled = false;
      renderHex(currentData, hexView);
      detectAndSummarize(currentData, summary);
    };
    reader.readAsArrayBuffer(file);
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

// Render hex dump in the pre element
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

// Client-side format detection and summary
function detectAndSummarize(data, element) {
  if (data.length < 4) {
    element.style.display = 'none';
    return;
  }

  const magic = (data[0] << 24) | (data[1] << 16) | (data[2] << 8) | data[3];
  let info = '';

  switch (magic) {
    case 0xa1b2c3d4:
    case 0xd4c3b2a1:
      info = 'Format: PCAP (packet capture)\nMagic: ' + magic.toString(16);
      if (data.length >= 24) {
        const view = new DataView(data.buffer, data.byteOffset);
        const swapped = magic === 0xd4c3b2a1;
        info += '\nVersion: ' + (swapped ? swap16(view.getUint16(4, false)) : view.getUint16(4, true));
        info += '.' + (swapped ? swap16(view.getUint16(6, false)) : view.getUint16(6, true));
        info += '\nSnap length: ' + (swapped ? swap32(view.getUint32(16, false)) : view.getUint32(16, true));
        info += '\nLink type: ' + (swapped ? swap32(view.getUint32(20, false)) : view.getUint32(20, true));
      }
      break;
    case 0x7f454c46:
      info = 'Format: ELF (Executable and Linkable Format)';
      if (data.length >= 20) {
        info += '\nClass: ' + (data[4] === 2 ? '64-bit' : '32-bit');
        info += '\nEndianness: ' + (data[5] === 1 ? 'little-endian' : 'big-endian');
        const types = { 1: 'relocatable', 2: 'executable', 3: 'shared object', 4: 'core' };
        const et = data[16] | (data[17] << 8);
        info += '\nType: ' + (types[et] || 'unknown (' + et + ')');
      }
      break;
    case 0xfeedface:
    case 0xfeedfacf:
    case 0xcefaedfe:
    case 0xcffaedfe:
      info = 'Format: Mach-O';
      if (data.length >= 16) {
        const is64 = (magic === 0xfeedfacf || magic === 0xcffaedfe);
        info += ' (' + (is64 ? '64-bit' : '32-bit') + ')';
        const view = new DataView(data.buffer, data.byteOffset);
        const cpu = view.getUint32(4, true);
        const cpuNames = { 7: 'x86', 12: 'arm', 0x01000007: 'x86-64', 0x0100000c: 'arm64' };
        info += '\nCPU: ' + (cpuNames[cpu] || 'other');
        const ft = view.getUint32(12, true);
        const ftNames = { 1: 'object', 2: 'executable', 6: 'dylib', 7: 'dylinker', 8: 'bundle' };
        info += '\nType: ' + (ftNames[ft] || 'type-' + ft);
        info += '\nLoad commands: ' + view.getUint32(16, true);
      }
      break;
    case 0xcafebabe:
    case 0xbebafeca:
      info = 'Format: Mach-O (universal/fat)';
      if (data.length >= 8) {
        const nArch = (data[4] << 24) | (data[5] << 16) | (data[6] << 8) | data[7];
        info += '\nArchitectures: ' + nArch;
        const cpuNames = { 7: 'x86', 12: 'arm', 0x01000007: 'x86-64', 0x0100000c: 'arm64' };
        for (let i = 0; i < nArch && i < 4; i++) {
          const off = 8 + i * 20;
          if (off + 4 > data.length) break;
          const cpu = (data[off] << 24) | (data[off+1] << 16) | (data[off+2] << 8) | data[off+3];
          info += '\n  ' + (cpuNames[cpu] || 'cpu-' + cpu);
        }
      }
      break;
    case 0x504b0304:
      info = 'Format: ZIP archive'; break;
    case 0x89504e47:
      info = 'Format: PNG image'; break;
    case 0x25504446:
      info = 'Format: PDF document'; break;
    default:
      if ((data[0] === 0x1f) && (data[1] === 0x8b)) {
        info = 'Format: gzip compressed';
      } else if ((data[0] === 0x4d) && (data[1] === 0x5a)) {
        info = 'Format: PE/COFF executable (Windows)';
      } else {
        info = 'Format: unknown binary\nSize: ' + data.length + ' bytes';
      }
  }

  element.textContent = info;
  element.style.display = 'block';
}

function swap16(v) { return ((v & 0xff) << 8) | ((v >> 8) & 0xff); }
function swap32(v) { return ((v & 0xff) << 24) | ((v & 0xff00) << 8) | ((v >> 8) & 0xff00) | ((v >> 24) & 0xff); }
