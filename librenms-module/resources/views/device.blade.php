@extends('layouts.librenmsv1')

@section('title', 'Rusted — ' . $hostname)

@section('content')
<div class="container-fluid">
    <h2>
        <i class="fa fa-floppy-o"></i> Rusted &mdash; {{ $hostname }}
        <a href="{{ route('rusted.index') }}" class="btn btn-xs btn-default pull-right" title="Back to Rusted Backups">
            <i class="fa fa-arrow-left"></i> Back
        </a>
    </h2>

    @include('rusted::partials.sticky-alerts', ['id' => 'rusted-alerts'])

    {{-- The info line is re-rendered by JS; the toolbar beside it is static so
         its buttons survive every re-render. --}}
    <div style="margin-bottom:12px">
        <span id="rusted-toolbar">
            <button id="rusted-backup-now" class="btn btn-xs btn-primary" type="button" disabled>
                <i class="fa fa-download"></i> Back up now
            </button>
        </span>
        <span class="text-muted" id="rusted-meta" style="margin-left:8px"><em>Loading device&hellip;</em></span>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Configuration</strong> <span id="rusted-config-rev" class="text-muted"></span>
            <span class="pull-right">
                <button id="rusted-config-refresh" class="btn btn-xs btn-default" type="button" title="Show the latest configuration"><i class="fa fa-refresh"></i> Latest</button>
                <button id="rusted-config-download" class="btn btn-xs btn-default" type="button" title="Download the configuration shown"><i class="fa fa-download"></i> Raw</button>
                <button id="rusted-config-history" class="btn btn-xs btn-default" type="button" title="Download every stored revision as a zip"><i class="fa fa-file-archive-o"></i> All revisions</button>
            </span>
        </div>
        <div class="panel-body">
            <pre id="rusted-config" style="max-height:500px;overflow:auto;margin:0"><em>Loading&hellip;</em></pre>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Revision history</strong>
            <button id="rusted-versions-refresh" class="btn btn-xs btn-default pull-right" type="button"><i class="fa fa-refresh"></i></button>
        </div>
        <div class="panel-body">
            <table class="table table-condensed table-striped" style="margin-bottom:0">
                <thead>
                    <tr><th>Commit</th><th>Date</th><th>Subject</th><th class="text-right">Actions</th></tr>
                </thead>
                <tbody id="rusted-versions">
                    <tr><td colspan="4"><em>Loading&hellip;</em></td></tr>
                </tbody>
            </table>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading"><strong>Compare revisions</strong></div>
        <div class="panel-body">
            <div class="form-inline" style="margin-bottom:8px">
                <select class="form-control input-sm" id="rusted-diff-from" style="width:260px"></select>
                <select class="form-control input-sm" id="rusted-diff-to" style="width:260px"></select>
                <button id="rusted-diff-swap" class="btn btn-xs btn-default" type="button" title="Swap"><i class="fa fa-exchange"></i></button>
                <button id="rusted-diff-run" class="btn btn-xs btn-primary" type="button">Diff</button>
            </div>
            <pre id="rusted-diff" style="max-height:500px;overflow:auto;margin:0;background:#fff"><em>Pick a revision (and optionally a second one) and press Diff.</em></pre>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Backup runs</strong>
            <button id="rusted-runs-refresh" class="btn btn-xs btn-default pull-right" type="button"><i class="fa fa-refresh"></i></button>
        </div>
        <div class="panel-body">
            <table class="table table-condensed table-striped" style="margin-bottom:0">
                <thead>
                    <tr><th>Started</th><th>Status</th><th>Bytes</th><th>Commit</th><th>Detail</th></tr>
                </thead>
                <tbody id="rusted-runs">
                    <tr><td colspan="5"><em>Loading&hellip;</em></td></tr>
                </tbody>
            </table>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        index: @json(route('rusted.index')),
        csrf: @json(csrf_token()),
        host: @json($hostname),
    };
    const host = encodeURIComponent(RUSTED.host);

    const $alerts = document.getElementById('rusted-alerts');
    const $meta = document.getElementById('rusted-meta');
    const $backupBtn = document.getElementById('rusted-backup-now');
    const $config = document.getElementById('rusted-config');
    const $configRev = document.getElementById('rusted-config-rev');
    const $diff = document.getElementById('rusted-diff');
    const $versions = document.getElementById('rusted-versions');
    const $runs = document.getElementById('rusted-runs');
    const $from = document.getElementById('rusted-diff-from');
    const $to = document.getElementById('rusted-diff-to');

    // What is currently in $config, so "Raw" downloads exactly that.
    let shownConfig = null;
    let shownCommit = '';

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    }

    function alertBox(type, msg) {
        window.rustedAlert($alerts, type, msg);
    }

    function label(text, kind) {
        return '<span class="label label-' + kind + '">' + esc(text) + '</span>';
    }

    function errorOf(r) {
        return (r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status);
    }

    async function apiText(path, query) {
        const res = await fetch(RUSTED.base + path + (query ? '?' + query : ''), {
            method: 'GET',
            credentials: 'same-origin',
            headers: { 'X-Requested-With': 'XMLHttpRequest' },
        });
        return { ok: res.ok, status: res.status, text: await res.text() };
    }

    async function apiJson(method, path, body) {
        const opts = { method: method, credentials: 'same-origin',
            headers: { 'Accept': 'application/json', 'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-TOKEN': RUSTED.csrf } };
        if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
        const res = await fetch(RUSTED.base + path, opts);
        let data = null;
        try { data = await res.json(); } catch (e) { /* non-JSON */ }
        return { ok: res.ok, status: res.status, data: data };
    }

    async function loadDevice() {
        const r = await apiJson('GET', '/devices/' + host);
        if (r.status === 404) {
            $meta.innerHTML = '<span class="text-warning">This device is not managed by rusted. ' +
                '<a href="' + esc(RUSTED.index) + '">Open Rusted Backups</a> to add it.</span>';
            $backupBtn.disabled = true;
            $config.textContent = '(not managed)';
            return false;
        }
        if (!r.ok) {
            $meta.innerHTML = '<span class="text-danger">Rusted API error: ' + esc(errorOf(r)) + '</span>';
            return false;
        }
        const d = r.data || {};
        $meta.innerHTML =
            'Driver: <code>' + esc(d.driver || '—') + '</code>' +
            ' &middot; Transport: <code>' + esc(d.transport || 'default') + '</code>' +
            (d.group ? ' &middot; Group: <code>' + esc(d.group) + '</code>' : '') +
            ' &middot; ' + (d.enabled === false ? label('disabled', 'default') : label('enabled', 'success'));
        $backupBtn.disabled = false;
        return true;
    }

    async function loadConfig(commit) {
        $config.innerHTML = '<em>Loading&hellip;</em>';
        shownConfig = null;
        shownCommit = commit || '';
        $configRev.textContent = commit ? '@ ' + commit.substring(0, 8) : '(latest)';
        const r = await apiText('/devices/' + host + '/config', commit ? 'commit=' + encodeURIComponent(commit) : '');
        if (!r.ok) {
            $config.textContent = r.status === 404 ? '(no backup yet)' : ('error: HTTP ' + r.status);
            return;
        }
        shownConfig = r.text;
        $config.textContent = r.text === '' ? '(empty)' : r.text;
    }

    async function loadVersions() {
        $versions.innerHTML = '<tr><td colspan="4"><em>Loading&hellip;</em></td></tr>';
        $from.innerHTML = '<option value="" disabled selected>revision</option>';
        $to.innerHTML = '<option value="" selected>(its parent change only)</option>';
        const r = await apiJson('GET', '/devices/' + host + '/versions');
        if (!r.ok) {
            $versions.innerHTML = '<tr><td colspan="4"><em>Could not load revisions.</em></td></tr>';
            return;
        }
        const versions = Array.isArray(r.data) ? r.data : [];
        if (!versions.length) {
            $versions.innerHTML = '<tr><td colspan="4"><em>No revisions yet.</em></td></tr>';
            return;
        }
        $versions.innerHTML = versions.map(function (v) {
            const c = esc(v.commit);
            return '<tr>' +
                '<td><code>' + esc((v.commit || '').substring(0, 8)) + '</code></td>' +
                '<td>' + esc(v.date) + '</td>' +
                '<td>' + esc(v.subject) + '</td>' +
                '<td class="text-right">' +
                    '<button class="btn btn-xs btn-default" type="button" data-commit="' + c + '" title="View this revision"><i class="fa fa-eye"></i></button> ' +
                    '<button class="btn btn-xs btn-default" type="button" data-diff="' + c + '" title="What this revision changed"><i class="fa fa-exchange"></i></button>' +
                '</td></tr>';
        }).join('');

        versions.forEach(function (v) {
            const opt = document.createElement('option');
            opt.value = v.commit;
            opt.textContent = (v.commit || '').substring(0, 8) + ' — ' + (v.subject || '') + ' (' + (v.date || '') + ')';
            $from.appendChild(opt.cloneNode(true));
            $to.appendChild(opt);
        });
        $from.value = versions[0].commit;
        $to.value = '';
    }

    async function loadRuns() {
        $runs.innerHTML = '<tr><td colspan="5"><em>Loading&hellip;</em></td></tr>';
        const r = await apiJson('GET', '/devices/' + host + '/history');
        if (!r.ok) {
            $runs.innerHTML = '<tr><td colspan="5"><em>Could not load backup runs.</em></td></tr>';
            return;
        }
        const runs = Array.isArray(r.data) ? r.data : [];
        if (!runs.length) {
            $runs.innerHTML = '<tr><td colspan="5"><em>No backup runs recorded.</em></td></tr>';
            return;
        }
        $runs.innerHTML = runs.map(function (run) {
            const s = run.status || '';
            const kind = s === 'success' ? 'success' : (s === 'failed' ? 'danger' : (s === 'unchanged' ? 'info' : 'default'));
            return '<tr>' +
                '<td>' + esc(run.started_at) + '</td>' +
                '<td>' + label(s, kind) + '</td>' +
                '<td>' + esc(run.bytes || 0) + '</td>' +
                '<td><code>' + esc((run.commit || '').substring(0, 8)) + '</code></td>' +
                '<td>' + esc(run.message) + '</td>' +
            '</tr>';
        }).join('');
    }

    async function loadDiff() {
        const from = $from.value;
        const to = $to.value;
        if (!from) { $diff.textContent = 'Pick a revision first.'; return; }
        $diff.innerHTML = '<em>Computing diff&hellip;</em>';
        const query = 'from=' + encodeURIComponent(from) + (to ? '&to=' + encodeURIComponent(to) : '');
        const r = await apiText('/devices/' + host + '/diff', query);
        if (!r.ok) {
            $diff.textContent = r.status === 404 ? '(no such revision)' : ('error: HTTP ' + r.status);
            return;
        }
        renderDiff(r.text);
    }

    function renderDiff(text) {
        if (!text) { $diff.textContent = '(no differences)'; return; }
        $diff.innerHTML = String(text).split('\n').map(function (l) {
            const c = esc(l);
            if (l.startsWith('diff ') || l.startsWith('index ') || l.startsWith('+++') || l.startsWith('---'))
                return '<span class="text-muted">' + c + '</span>';
            if (l.startsWith('@@')) return '<span class="text-info">' + c + '</span>';
            if (l.startsWith('+')) return '<span class="text-success">' + c + '</span>';
            if (l.startsWith('-')) return '<span class="text-danger">' + c + '</span>';
            return c;
        }).join('\n');
    }

    async function doBackup() {
        const original = $backupBtn.innerHTML;
        $backupBtn.disabled = true;
        $backupBtn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Backing up&hellip;';
        const r = await apiJson('POST', '/devices/' + host + '/backup');
        $backupBtn.disabled = false;
        $backupBtn.innerHTML = original;
        if (!r.ok) {
            alertBox('danger', 'Backup failed: ' + errorOf(r));
        } else {
            const st = (r.data && r.data.status) || 'done';
            const msg = (r.data && r.data.message) || '';
            alertBox(st === 'failed' ? 'danger' : 'success', 'Backup ' + st + (msg ? ' — ' + msg : ''));
        }
        loadDevice();
        loadVersions();
        loadRuns();
        if (!shownCommit) loadConfig();
    }

    function safeFileName(s) {
        return String(s).replace(/[^A-Za-z0-9._-]+/g, '_');
    }

    function downloadConfig() {
        if (shownConfig === null) {
            alertBox('warning', 'No configuration loaded to download.');
            return;
        }
        const blob = new Blob([shownConfig], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = safeFileName(RUSTED.host) + (shownCommit ? '-' + shownCommit.substring(0, 8) : '') + '.cfg';
        document.body.appendChild(a);
        a.click();
        setTimeout(() => { URL.revokeObjectURL(url); a.remove(); }, 1000);
    }

    $backupBtn.addEventListener('click', doBackup);
    document.getElementById('rusted-config-refresh').addEventListener('click', () => loadConfig());
    document.getElementById('rusted-config-download').addEventListener('click', downloadConfig);
    document.getElementById('rusted-config-history').addEventListener('click', function () {
        // A plain navigation: the server sends the zip as an attachment.
        window.location.href = RUSTED.base + '/devices/' + host + '/history/zip';
    });
    document.getElementById('rusted-versions-refresh').addEventListener('click', loadVersions);
    document.getElementById('rusted-runs-refresh').addEventListener('click', loadRuns);

    document.getElementById('rusted-diff-swap').addEventListener('click', function () {
        const a = $from.value, b = $to.value;
        if (!b) return; // nothing to swap with "(its parent change only)"
        $from.value = b; $to.value = a;
        loadDiff();
    });
    document.getElementById('rusted-diff-run').addEventListener('click', loadDiff);

    $versions.addEventListener('click', function (e) {
        const view = e.target.closest('button[data-commit]');
        if (view) { loadConfig(view.getAttribute('data-commit')); return; }
        const diffBtn = e.target.closest('button[data-diff]');
        if (diffBtn) {
            $from.value = diffBtn.getAttribute('data-diff');
            $to.value = '';
            loadDiff();
        }
    });

    loadDevice().then(function (managed) {
        if (!managed) return;
        loadConfig();
        loadVersions();
        loadRuns();
    });
})();
</script>
@endsection
