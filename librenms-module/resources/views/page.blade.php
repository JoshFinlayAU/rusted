@extends('layouts.librenmsv1')

@section('title', 'Rusted Backups')

@section('content')
<div class="container-fluid">
    <h2><i class="fa fa-floppy-o"></i> Rusted &mdash; Configuration Backups</h2>

    <div id="rusted-alerts"></div>

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Devices</strong>
            <button id="rusted-refresh" class="btn btn-xs btn-default pull-right" type="button">
                <i class="fa fa-refresh"></i> Refresh
            </button>
        </div>
        <div class="panel-body">
            <table class="table table-condensed table-hover">
                <thead>
                    <tr>
                        <th>Name</th><th>Host</th><th>Port</th>
                        <th>Driver</th><th>Group</th><th>Enabled</th><th>Actions</th>
                    </tr>
                </thead>
                <tbody id="rusted-devices">
                    <tr><td colspan="7"><em>Loading&hellip;</em></td></tr>
                </tbody>
            </table>
        </div>
    </div>

    <div id="rusted-history-panel" class="panel panel-info" style="display:none">
        <div class="panel-heading">
            <strong>Backup history: <span id="rusted-history-device"></span></strong>
            <button id="rusted-history-close" class="btn btn-xs btn-default pull-right" type="button">close</button>
        </div>
        <div class="panel-body">
            <table class="table table-condensed table-striped">
                <thead>
                    <tr><th>Started</th><th>Status</th><th>Bytes</th><th>Commit</th><th>Detail</th></tr>
                </thead>
                <tbody id="rusted-history"></tbody>
            </table>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading"><strong>Add device</strong></div>
        <div class="panel-body">
            <form id="rusted-add" class="form-inline">
                <input class="form-control input-sm" type="text" name="name" placeholder="name" required>
                <input class="form-control input-sm" type="text" name="host" placeholder="host / IP" required>
                <input class="form-control input-sm" type="number" name="port" placeholder="22" style="width:80px">
                <select class="form-control input-sm" name="driver" id="rusted-driver" required>
                    <option value="" disabled selected>driver</option>
                </select>
                <input class="form-control input-sm" type="text" name="credential" placeholder="credential" required>
                <input class="form-control input-sm" type="text" name="group" placeholder="group (optional)">
                <button class="btn btn-sm btn-success" type="submit"><i class="fa fa-plus"></i> Add</button>
            </form>
            <p class="help-block" style="margin-top:8px">
                Credentials are managed with the <code>rusted cred</code> CLI; reference an existing credential by name.
            </p>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        csrf: @json(csrf_token()),
    };

    const $devices = document.getElementById('rusted-devices');
    const $alerts = document.getElementById('rusted-alerts');
    const $driver = document.getElementById('rusted-driver');
    const $form = document.getElementById('rusted-add');
    const $historyPanel = document.getElementById('rusted-history-panel');
    const $history = document.getElementById('rusted-history');
    const $historyDevice = document.getElementById('rusted-history-device');

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    }

    function alertBox(type, msg) {
        const div = document.createElement('div');
        div.className = 'alert alert-' + type + ' alert-dismissable';
        div.innerHTML = '<button type="button" class="close" data-dismiss="alert">&times;</button>' + esc(msg);
        $alerts.appendChild(div);
        setTimeout(() => div.remove(), 8000);
    }

    async function api(method, path, body) {
        const opts = {
            method: method,
            credentials: 'same-origin',
            headers: {
                'Accept': 'application/json',
                'X-Requested-With': 'XMLHttpRequest',
                'X-CSRF-TOKEN': RUSTED.csrf,
            },
        };
        if (body !== undefined) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        }
        const res = await fetch(RUSTED.base + path, opts);
        let data = null;
        try { data = await res.json(); } catch (e) { /* non-JSON */ }
        if (!res.ok) {
            let msg = (data && (data.error || data.message)) || ('HTTP ' + res.status);
            if (data && data.errors) {
                msg = Object.values(data.errors).flat().join(' ');
            }
            throw new Error(msg);
        }
        return data;
    }

    function label(text, kind) {
        return '<span class="label label-' + kind + '">' + esc(text) + '</span>';
    }

    function renderDevices(devices) {
        if (!devices || !devices.length) {
            $devices.innerHTML = '<tr><td colspan="7"><em>No devices yet.</em></td></tr>';
            return;
        }
        $devices.innerHTML = devices.map(function (d) {
            const enabled = d.enabled ? label('yes', 'success') : label('no', 'default');
            const n = esc(d.name);
            return '<tr>' +
                '<td>' + esc(d.name) + '</td>' +
                '<td>' + esc(d.host) + '</td>' +
                '<td>' + esc(d.port) + '</td>' +
                '<td>' + esc(d.driver) + '</td>' +
                '<td>' + (d.group ? esc(d.group) : '-') + '</td>' +
                '<td>' + enabled + '</td>' +
                '<td>' +
                    '<button class="btn btn-xs btn-primary" data-action="backup" data-name="' + n + '"><i class="fa fa-download"></i> Backup</button> ' +
                    '<button class="btn btn-xs btn-default" data-action="history" data-name="' + n + '"><i class="fa fa-history"></i> History</button> ' +
                    '<button class="btn btn-xs btn-danger" data-action="remove" data-name="' + n + '"><i class="fa fa-trash"></i> Remove</button>' +
                '</td>' +
            '</tr>';
        }).join('');
    }

    async function loadDevices() {
        try {
            renderDevices(await api('GET', '/devices'));
        } catch (e) {
            $devices.innerHTML = '<tr><td colspan="7"><em>Could not load devices.</em></td></tr>';
            alertBox('warning', e.message);
        }
    }

    async function loadDrivers() {
        try {
            const drivers = await api('GET', '/drivers');
            (drivers || []).forEach(function (drv) {
                const opt = document.createElement('option');
                opt.value = drv.name;
                opt.textContent = drv.name;
                $driver.appendChild(opt);
            });
        } catch (e) {
            alertBox('warning', 'Could not load drivers: ' + e.message);
        }
    }

    async function doBackup(name, btn) {
        const original = btn.innerHTML;
        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Backing up&hellip;';
        try {
            const r = await api('POST', '/devices/' + encodeURIComponent(name) + '/backup');
            const status = r.status || 'done';
            const detail = r.message || '';
            const kind = status === 'failed' ? 'danger' : 'success';
            alertBox(kind, 'Backup of ' + name + ': ' + status + (detail ? ' — ' + detail : ''));
        } catch (e) {
            alertBox('danger', 'Backup of ' + name + ' failed: ' + e.message);
        } finally {
            btn.disabled = false;
            btn.innerHTML = original;
        }
    }

    async function doRemove(name) {
        if (!confirm('Remove ' + name + '?')) return;
        try {
            await api('DELETE', '/devices/' + encodeURIComponent(name));
            alertBox('success', 'Device ' + name + ' removed.');
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Remove failed: ' + e.message);
        }
    }

    async function showHistory(name) {
        $historyDevice.textContent = name;
        $history.innerHTML = '<tr><td colspan="5"><em>Loading&hellip;</em></td></tr>';
        $historyPanel.style.display = '';
        try {
            const runs = await api('GET', '/devices/' + encodeURIComponent(name) + '/history');
            if (!runs || !runs.length) {
                $history.innerHTML = '<tr><td colspan="5"><em>No backup runs recorded.</em></td></tr>';
                return;
            }
            $history.innerHTML = runs.map(function (r) {
                const s = r.status || '';
                const kind = s === 'success' ? 'success' : (s === 'failed' ? 'danger' : 'default');
                const commit = (r.commit || '').substring(0, 8);
                return '<tr>' +
                    '<td>' + esc(r.started_at) + '</td>' +
                    '<td>' + label(s, kind) + '</td>' +
                    '<td>' + esc(r.bytes || 0) + '</td>' +
                    '<td><code>' + esc(commit) + '</code></td>' +
                    '<td>' + esc(r.message) + '</td>' +
                '</tr>';
            }).join('');
        } catch (e) {
            $history.innerHTML = '<tr><td colspan="5"><em>Could not load history.</em></td></tr>';
            alertBox('warning', e.message);
        }
    }

    // Event delegation for per-row action buttons.
    $devices.addEventListener('click', function (e) {
        const btn = e.target.closest('button[data-action]');
        if (!btn) return;
        const name = btn.getAttribute('data-name');
        if (btn.dataset.action === 'backup') doBackup(name, btn);
        else if (btn.dataset.action === 'remove') doRemove(name);
        else if (btn.dataset.action === 'history') showHistory(name);
    });

    document.getElementById('rusted-refresh').addEventListener('click', loadDevices);
    document.getElementById('rusted-history-close').addEventListener('click', function () {
        $historyPanel.style.display = 'none';
    });

    $form.addEventListener('submit', async function (e) {
        e.preventDefault();
        const fd = new FormData($form);
        const payload = {
            name: fd.get('name'),
            host: fd.get('host'),
            driver: fd.get('driver'),
            credential: fd.get('credential'),
            group: fd.get('group') || '',
        };
        const port = fd.get('port');
        if (port) payload.port = parseInt(port, 10);
        try {
            await api('POST', '/devices', payload);
            alertBox('success', 'Device ' + payload.name + ' added.');
            $form.reset();
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Add failed: ' + e.message);
        }
    });

    // Initial load.
    loadDrivers();
    loadDevices();
})();
</script>
@endsection
