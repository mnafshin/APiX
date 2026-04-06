// APiX Traffic Inspector — WebView Script
// This script runs inside the VS Code webview (sandboxed context).
// Communication with the extension host happens via acquireVsCodeApi().

(function () {
    'use strict';

    const vscode = acquireVsCodeApi();

    /** @type {Map<string, object>} requestId → transaction */
    const transactions = new Map();
    let selectedId = null;

    const listEl = document.getElementById('transaction-list');
    const detailEl = document.getElementById('detail-pane');
    const filterEl = document.getElementById('filter-input');

    // ── Message handling ──────────────────────────────────────────────────────

    window.addEventListener('message', (event) => {
        const msg = event.data;
        switch (msg.type) {
            case 'transaction':
                addTransaction(msg.data);
                break;
            case 'clear':
                transactions.clear();
                renderList();
                renderDetail(null);
                break;
        }
    });

    // ── Rendering ─────────────────────────────────────────────────────────────

    function addTransaction(tx) {
        transactions.set(tx.id, tx);
        renderList();
        if (selectedId === tx.id) {
            renderDetail(tx);
        }
    }

    function renderList() {
        const filter = filterEl.value.toLowerCase();
        listEl.innerHTML = '';
        for (const tx of [...transactions.values()].reverse()) {
            if (filter && !tx.request.url.toLowerCase().includes(filter)) { continue; }
            listEl.appendChild(buildRow(tx));
        }
    }

    function buildRow(tx) {
        const row = document.createElement('div');
        row.className = 'transaction-row' + (tx.id === selectedId ? ' selected' : '');
        row.dataset.id = tx.id;

        const method = document.createElement('span');
        method.className = 'method-badge';
        method.textContent = tx.request.method;

        const status = document.createElement('span');
        const code = tx.response?.statusCode ?? 0;
        status.className = `status-badge status-${Math.floor(code / 100)}xx`;
        status.textContent = code || '…';

        const url = document.createElement('span');
        url.className = 'url-text';
        url.textContent = tx.request.url;

        row.appendChild(method);
        row.appendChild(status);
        row.appendChild(url);

        row.addEventListener('click', () => selectTransaction(tx.id));
        return row;
    }

    function selectTransaction(id) {
        selectedId = id;
        renderList();
        renderDetail(transactions.get(id));
    }

    function renderDetail(tx) {
        if (!tx) {
            detailEl.innerHTML = '<p style="padding:10px;opacity:0.6">Select a request to inspect it.</p>';
            return;
        }
        // TODO: implement full detail view with tabs: Headers / Body / Response
        detailEl.innerHTML = `
            <h3>${tx.request.method} ${tx.request.url}</h3>
            <p>Status: ${tx.response?.statusCode ?? 'pending'} — ${tx.durationMs ?? 0}ms</p>
            <h4>Request Headers</h4>
            <pre class="body-preview">${JSON.stringify(tx.request.headers, null, 2)}</pre>
            <h4>Request Body</h4>
            <pre class="body-preview">${tx.request.body || '(empty)'}</pre>
            <h4>Response Headers</h4>
            <pre class="body-preview">${JSON.stringify(tx.response?.headers ?? {}, null, 2)}</pre>
            <h4>Response Body</h4>
            <pre class="body-preview">${tx.response?.body || '(empty)'}</pre>
            <button class="toolbar-btn" onclick="replaySelected()">Replay</button>
        `;
    }

    // ── Actions ───────────────────────────────────────────────────────────────

    window.replaySelected = function () {
        if (selectedId) {
            vscode.postMessage({ type: 'replay', data: { requestId: selectedId } });
        }
    };

    filterEl.addEventListener('input', renderList);

    // Initial empty state
    renderDetail(null);
})();
