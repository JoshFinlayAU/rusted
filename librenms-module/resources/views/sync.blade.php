@extends('layouts.librenmsv1')

@section('title', 'Rusted — Bulk device sync')

@section('content')
<div class="container-fluid">
    <h2>
        <i class="fa fa-exchange"></i> Rusted &mdash; Bulk device sync
        <a href="{{ route('rusted.index') }}" class="btn btn-xs btn-default pull-right" title="Back to Rusted Backups">
            <i class="fa fa-arrow-left"></i> Back
        </a>
    </h2>

    <div id="sync-alerts"></div>

    <div class="row">
        <!-- LEFT: LibreNMS devices not yet in rusted -->
        <div class="col-md-4">
            <input id="sync-left-filter" class="form-control input-sm" type="text"
                   placeholder="regex filter (hostname / os)">
            <div id="sync-left" class="list-group" style="max-height:500px;overflow:auto;margin-top:4px">
                <div class="list-group-item"><em>Loading&hellip;</em></div>
            </div>
        </div>

        <!-- CENTER: arrows + driver/credential selects (near the add arrow) -->
        <div class="col-md-4" style="text-align:center;padding-top:38px">
            <div class="form-group">
                <label>Driver</label>
                <select id="sync-driver" class="form-control input-sm"></select>
                <p class="help-block" style="margin-top:4px">One driver applied to the whole batch (per family).</p>
            </div>
            <div class="form-group">
                <label>Credential</label>
                <select id="sync-credential" class="form-control input-sm"></select>
            </div>
            <button id="sync-add" class="btn btn-success btn-sm"><i class="fa fa-arrow-right"></i> Add to rusted</button>
            <button id="sync-remove" class="btn btn-danger btn-sm"><i class="fa fa-arrow-left"></i> Remove from rusted</button>
            <p class="text-muted" style="margin-top:8px;font-size:12px">
                Removing a device also deletes its backup history. A zip snapshot
                of every revision is generated and downloaded first, so history can
                be recovered if removed by accident.
            </p>
        </div>

        <!-- RIGHT: rusted devices also in LibreNMS (selectable) + orphans (greyed) -->
        <div class="col-md-4">
            <input id="sync-right-filter" class="form-control input-sm" type="text"
                   placeholder="regex filter (hostname / driver)">
            <div id="sync-right" class="list-group" style="max-height:500px;overflow:auto;margin-top:4px">
                <div class="list-group-item"><em>Loading&hellip;</em></div>
            </div>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        csrf: @json(csrf_token()),
    };
    const leftFilter = document.getElementById('sync-left-filter');
    const rightFilter = document.getElementById('sync-right-filter');
    const driverSel = document.getElementById('sync-driver');
    const credSel = document.getElementById('sync-credential');

    // Devices that failed to add/remove on the last action, kept highlighted red.
    const errorSet = new Set();

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    }

    function alertBox(type, msg) {
        const d = document.getElementById('sync-alerts');
        const div = document.createElement('div');
        div.className = 'alert alert-' + type + ' alert-dismissable';
        div.innerHTML = '<button type="button" class="close" data-dismiss="alert">&times;</button>' + esc(msg);
        d.appendChild(div);
        setTimeout(() => div.remove(), 10000);
    }

    async function apiJson(method, path, body) {
        const opts = { method, credentials: 'same-origin',
            headers: { 'Accept': 'application/json', 'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-TOKEN': RUSTED.csrf } };
        if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
        const res = await fetch(RUSTED.base + path, opts);
        let data = null;
        try { data = await res.json(); } catch (e) { /* non-JSON */ }
        return { ok: res.ok, status: res.status, data: data };
    }

    function buildRegex(value) {
        value = (value || '').trim();
        if (!value) return null;
        try { return new RegExp(value, 'i'); } catch (e) { return null; }
    }

    function filterList(listId, re) {
        const items = document.querySelectorAll('#' + listId + ' .list-group-item');
        items.forEach(item => {
            if (!re) { item.style.display = ''; return; }
            const txt = (item.getAttribute('data-host') || '') + ' ' + (item.getAttribute('data-meta') || '');
            item.style.display = re.test(txt) ? '' : 'none';
        });
    }

    leftFilter.addEventListener('input', () => filterList('sync-left', buildRegex(leftFilter.value)));
    rightFilter.addEventListener('input', () => filterList('sync-right', buildRegex(rightFilter.value)));

    function hostOf(item) { return item.getAttribute('data-host') || ''; }

    function renderLeft(items) {
        const box = document.getElementById('sync-left');
        if (!items || !items.length) {
            box.innerHTML = '<div class="list-group-item"><em>No LibreNMS devices to add.</em></div>';
            return;
        }
        box.innerHTML = items.map(d => {
            const h = esc(d.name);
            const yn = d.disabled ? ' <span class="label label-warning">disabled</span>' : (d.status ? '' : ' <span class="label label-danger">down</span>');
            return '<div class="list-group-item" data-host="' + esc(d.name) + '" data-meta="' + esc(d.os || '') + '">'
                + '<label style="margin:0"><input type="checkbox" class="sync-check"> '
                + h + yn + '</label></div>';
        }).join('');
    }

    function renderRight(right, grey) {
        const box = document.getElementById('sync-right');
        if ((!right || !right.length) && (!grey || !grey.length)) {
            box.innerHTML = '<div class="list-group-item"><em>No devices managed by rusted yet.</em></div>';
            return;
        }
        const rows = (right || []).map(d => {
            const h = esc(d.name);
            return '<div class="list-group-item" data-host="' + esc(d.name) + '" data-meta="' + esc(d.driver || '') + '">'
                + '<label style="margin:0"><input type="checkbox" class="sync-check"> '
                + h + ' <small class="text-muted">(' + esc(d.driver || '') + ')</small></label></div>';
        });
        const greys = (grey || []).map(d => {
            const h = esc(d.name);
            return '<div class="list-group-item list-group-item-default" data-host="' + esc(d.name) + '" data-meta="' + esc(d.driver || '') + '">'
                + '<label style="margin:0"><input type="checkbox" class="sync-check" disabled> '
                + h + ' <small class="text-muted">(' + esc(d.driver || '') + ')</small> '
                + '<span class="text-muted" title="Not present in LibreNMS — not selectable">ⓘ not in LibreNMS</span></label></div>';
        });
        box.innerHTML = rows.concat(greys).join('');
    }

    function applyErrors() {
        if (!errorSet.size) return;
        document.querySelectorAll('#sync-left .list-group-item, #sync-right .list-group-item').forEach(item => {
            item.classList.toggle('list-group-item-danger', errorSet.has(hostOf(item)));
        });
    }

    function selectedIn(listId) {
        return Array.from(document.querySelectorAll('#' + listId + ' .sync-check:checked'))
            .map(cb => hostOf(cb.closest('.list-group-item')));
    }

    async function loadDrivers() {
        const r = await apiJson('GET', '/drivers');
        const drivers = (r.ok && Array.isArray(r.data)) ? r.data : [];
        driverSel.innerHTML = '<option value="" disabled selected>driver</option>'
            + drivers.map(d => '<option value="' + esc(d.name) + '">' + esc(d.name) + ' — ' + esc(d.description || '') + '</option>').join('');
    }

    async function loadCredentials() {
        const r = await apiJson('GET', '/credentials');
        const creds = (r.ok && Array.isArray(r.data)) ? r.data : [];
        credSel.innerHTML = '<option value="" disabled selected>credential</option>'
            + creds.map(c => '<option value="' + esc(c.name) + '">' + esc(c.name) + ' (' + esc(c.username) + ')</option>').join('');
    }

    async function loadState() {
        const r = await apiJson('GET', '/sync/state');
        if (!r.ok) {
            alertBox('danger', 'Could not load sync state: ' + ((r.data && r.data.error) || ('HTTP ' + r.status)));
            return;
        }
        renderLeft(r.data.left || []);
        renderRight(r.data.right || [], r.data.grey || []);
        applyErrors();
    }

    document.getElementById('sync-add').addEventListener('click', async () => {
        const devices = selectedIn('sync-left');
        if (!devices.length) return alertBox('warning', 'Select devices from the left list.');
        const driver = driverSel.value;
        const credential = credSel.value;
        if (!driver) return alertBox('warning', 'Pick a driver.');
        if (!credential) return alertBox('warning', 'Pick a credential.');

        document.getElementById('sync-add').disabled = true;
        document.getElementById('sync-add').innerHTML = '<i class="fa fa-spinner fa-spin"></i> Adding&hellip;';
        const r = await apiJson('POST', '/sync/add', { driver: driver, credential: credential, devices: devices });
        document.getElementById('sync-add').disabled = false;
        document.getElementById('sync-add').innerHTML = '<i class="fa fa-arrow-right"></i> Add to rusted';

        if (r.ok) {
            if (r.data && r.data.failed && r.data.failed.length) {
                r.data.failed.forEach(f => errorSet.add(f.name));
            }
            if (r.data && r.data.added && r.data.added.length) {
                r.data.added.forEach(n => errorSet.delete(n));
                alertBox('success', 'Added ' + r.data.added.length + ' device(s) to rusted.');
            }
            if (r.data && r.data.failed && r.data.failed.length) {
                alertBox('danger', r.data.failed.length + ' device(s) failed to add; they stay on the left, highlighted red.');
            }
        } else {
            alertBox('danger', 'Add failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
        }
        loadState();
    });

    document.getElementById('sync-remove').addEventListener('click', async () => {
        const devices = selectedIn('sync-right');
        if (!devices.length) return alertBox('warning', 'Select devices from the right list (greyed rows are not selectable).');

        const msg = 'Remove ' + devices.length + ' device(s) from rusted?\n\n'
            + 'This deletes them and their configuration history from rusted.\n'
            + 'A zip snapshot of every revision will be downloaded first, so the\n'
            + 'history can be recovered if this was a mistake.\n\n'
            + 'Confirm to proceed.';
        if (!confirm(msg)) return;

        document.getElementById('sync-remove').disabled = true;
        document.getElementById('sync-remove').innerHTML = '<i class="fa fa-spinner fa-spin"></i> Removing&hellip;';
        const r = await apiJson('POST', '/sync/remove', { devices: devices });
        document.getElementById('sync-remove').disabled = false;
        document.getElementById('sync-remove').innerHTML = '<i class="fa fa-arrow-left"></i> Remove from rusted';

        if (r.ok) {
            if (r.data && r.data.removed && r.data.removed.length) {
                r.data.removed.forEach(n => errorSet.delete(n));
            }
            if (r.data && r.data.failed) {
                // server returns "kept" for deletes that failed
            }
            if (r.data && r.data.token) {
                alertBox('info', 'Removed ' + (r.data.removed||[]).length + ' device(s). Downloading history snapshot&hellip;');
                window.location.href = @json(url('plugin/rusted/api/sync/history')) + '/' + encodeURIComponent(r.data.token);
            } else {
                alertBox('success', 'Removed ' + (r.data.removed||[]).length + ' device(s). No history to archive.');
            }
            if (r.data && r.data.kept && r.data.kept.length) {
                r.data.kept.forEach(k => errorSet.add(k.name));
                alertBox('warning', r.data.kept.length + ' device(s) could not be removed and remain managed.');
            }
        } else {
            alertBox('danger', 'Remove failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
        }
        loadState();
    });

    // Initial load.
    loadDrivers();
    loadCredentials();
    loadState();
})();
</script>
@endsection
