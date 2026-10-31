@extends('layouts.librenmsv1')

@section('title', 'Rusted Backups')

@section('content')
<div class="container-fluid">
    <h2><i class="fa fa-floppy-o"></i> Rusted &mdash; Configuration Backups
        <a href="{{ route('rusted.sync') }}" class="btn btn-xs btn-default pull-right" title="Bulk add / disable LibreNMS devices">
            <i class="fa fa-exchange"></i> Device sync
        </a>
    </h2>

    @include('rusted::partials.sticky-alerts', ['id' => 'rusted-alerts'])

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
                        <th>Driver</th><th>Transport</th><th>Credential</th><th>Group</th>
                        <th>Backup</th><th>Actions</th>
                    </tr>
                </thead>
                <tbody id="rusted-devices">
                    <tr><td colspan="9"><em>Loading&hellip;</em></td></tr>
                </tbody>
            </table>
            <p class="help-block">
                Port, driver, transport, credential and group are edited in place and saved as soon as
                they change. Clear the port or group to fall back to the default / no group.
            </p>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading"><strong>Add device</strong></div>
        <div class="panel-body">
            <form id="rusted-add" class="form-inline">
                <input class="form-control input-sm" type="text" name="name" placeholder="name" required>
                <input class="form-control input-sm" type="text" name="host" placeholder="host / IP" required>
                <input class="form-control input-sm" type="number" name="port" placeholder="22" min="1" max="65535" style="width:80px">
                <select class="form-control input-sm" name="driver" id="rusted-driver" required>
                    <option value="" disabled selected>driver</option>
                </select>
                <select class="form-control input-sm" name="transport" id="rusted-transport">
                    <option value="" selected>transport (default)</option>
                </select>
                <select class="form-control input-sm" name="credential" id="rusted-credential" required>
                    <option value="" disabled selected>credential</option>
                </select>
                <input class="form-control input-sm" type="text" name="group" placeholder="group (optional)">
                <button class="btn btn-sm btn-success" type="submit"><i class="fa fa-plus"></i> Add</button>
            </form>
            <p class="help-block" style="margin-top:8px">
                Pick a credential below, or add one with the Credentials panel.
            </p>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading"><strong>Credentials</strong></div>
        <div class="panel-body">
            <table class="table table-condensed table-hover">
                <thead>
                    <tr><th>Name</th><th>Username</th><th>Password</th><th>Key</th><th>Enable</th><th>Actions</th></tr>
                </thead>
                <tbody id="rusted-credentials">
                    <tr><td colspan="6"><em>Loading&hellip;</em></td></tr>
                </tbody>
            </table>
            <form id="rusted-cred-add" class="form-inline">
                <input class="form-control input-sm" type="text" name="name" placeholder="name" required>
                <input class="form-control input-sm" type="text" name="username" placeholder="username" required autocomplete="off">
                <input class="form-control input-sm" type="password" name="password" placeholder="password" autocomplete="new-password">
                <input class="form-control input-sm" type="password" name="enable" placeholder="enable secret (optional)" autocomplete="new-password">
                <button class="btn btn-sm btn-success" type="submit"><i class="fa fa-plus"></i> Add credential</button>
            </form>
            <p class="help-block" style="margin-top:8px">
                Passwords are sent once to rusted, encrypted at rest, and never returned to the browser.
            </p>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        device: @json(url('plugin/rusted/device')),
        csrf: @json(csrf_token()),
    };

    const $devices = document.getElementById('rusted-devices');
    const $alerts = document.getElementById('rusted-alerts');
    const $driver = document.getElementById('rusted-driver');
    const $transport = document.getElementById('rusted-transport');
    const $form = document.getElementById('rusted-add');
    const $credentials = document.getElementById('rusted-credentials');
    const $credForm = document.getElementById('rusted-cred-add');
    const $credential = document.getElementById('rusted-credential');

    // Option lists for the inline-edit dropdowns.
    let driverOptions = [];     // names
    let transportOptions = [];  // names
    let credentialOptions = []; // names

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    }

    function alertBox(type, msg) {
        window.rustedAlert($alerts, type, msg);
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
            const err = new Error(msg);
            err.status = res.status;
            throw err;
        }
        return data;
    }

    function label(text, kind) {
        return '<span class="label label-' + kind + '">' + esc(text) + '</span>';
    }

    // ---- Backup status indicator ----
    //   never backed up              -> grey circle "never"
    //   success/unchanged within 24h -> green check
    //   success/unchanged older      -> grey "?" (stale, not failed)
    //   last run failed              -> red triangle, message in the tooltip
    function backupStatus(d) {
        const ts = d.last_backup;
        const st = d.last_status;
        if (!ts) {
            return '<span class="text-muted" title="no backups yet"><i class="fa fa-circle-o"></i> never</span>';
        }
        const dt = new Date(ts);
        const when = isNaN(dt.getTime()) ? String(ts) : dt.toLocaleString();
        const ageH = (Date.now() - dt.getTime()) / 3600000;
        const ok = st === 'success' || st === 'unchanged';
        let icon, color, tip;
        if (st === 'failed') {
            icon = 'fa-exclamation-triangle'; color = 'text-danger';
            tip = 'Failed ' + when + (d.last_message ? ': ' + d.last_message : '');
        } else if (!ok) {
            icon = 'fa-question-circle'; color = 'text-muted';
            tip = (st ? st + ' ' : '') + when;
        } else if (ageH < 24) {
            icon = 'fa-check-circle'; color = 'text-success';
            tip = 'Up to date (' + when + ')';
        } else {
            icon = 'fa-question-circle'; color = 'text-muted';
            tip = 'Last backup ' + Math.floor(ageH) + 'h ago (' + when + ')';
        }
        // tip carries device-supplied text (last_message): always escape it.
        return '<span class="' + color + '" title="' + esc(tip) + '"><i class="fa ' + icon + '"></i></span>';
    }

    // A <select> for an inline-edited field. The device's current value is
    // always present (even if the option list lacks it) so the select never
    // silently shows a different value from the one stored.
    function editSelect(d, field, options, emptyLabel) {
        const current = d[field] == null ? '' : String(d[field]);
        const values = options.slice();
        if (current !== '' && values.indexOf(current) === -1) values.unshift(current);
        let html = '';
        if (emptyLabel !== null) {
            html += '<option value=""' + (current === '' ? ' selected' : '') + '>' + esc(emptyLabel) + '</option>';
        } else if (current === '') {
            html += '<option value="" selected disabled>(unknown)</option>';
        }
        html += values.map(v =>
            '<option value="' + esc(v) + '"' + (v === current ? ' selected' : '') + '>' + esc(v) + '</option>'
        ).join('');
        return '<select class="form-control input-sm rusted-edit" data-field="' + field + '" data-name="' + esc(d.name) + '">' + html + '</select>';
    }

    function editInput(d, field, type, placeholder, width) {
        const v = d[field] == null || d[field] === 0 ? '' : d[field];
        return '<input type="' + type + '" class="form-control input-sm rusted-edit" data-field="' + field + '" ' +
            'data-name="' + esc(d.name) + '" value="' + esc(v) + '" placeholder="' + esc(placeholder) + '" ' +
            'data-orig="' + esc(v) + '" style="width:' + width + '">';
    }

    // ---- Render devices ----
    function renderDevices(devices) {
        if (!devices || !devices.length) {
            $devices.innerHTML = '<tr><td colspan="9"><em>No devices yet.</em></td></tr>';
            return;
        }
        $devices.innerHTML = devices.map(function (d) {
            const n = esc(d.name);
            const isEnabled = (d.enabled !== false);
            const toggleClass = isEnabled ? 'btn-success' : 'btn-danger';
            const toggleTitle = isEnabled
                ? 'Enabled — click to disable (stops backups, keeps history)'
                : 'Disabled — click to enable (resumes backups)';
            return '<tr' + (isEnabled ? '' : ' class="text-muted"') + '>' +
                '<td><a href="' + esc(RUSTED.device + '/' + encodeURIComponent(d.name)) + '">' + n + '</a></td>' +
                '<td>' + esc(d.host) + '</td>' +
                '<td>' + editInput(d, 'port', 'number', '22', '75px') + '</td>' +
                '<td>' + editSelect(d, 'driver', driverOptions, null) + '</td>' +
                '<td>' + editSelect(d, 'transport', transportOptions, 'default') + '</td>' +
                '<td>' + editSelect(d, 'credential', credentialOptions, null) + '</td>' +
                '<td>' + editInput(d, 'group', 'text', '-', '100px') + '</td>' +
                '<td>' + backupStatus(d) + '</td>' +
                '<td><div class="btn-group btn-group-xs" role="group">' +
                    '<button class="btn btn-xs ' + toggleClass + '" data-action="toggle" data-name="' + n + '" data-enabled="' + (isEnabled ? '1' : '0') + '" title="' + toggleTitle + '"><i class="fa fa-power-off"></i></button>' +
                    '<button class="btn btn-xs btn-default" data-action="backup" data-name="' + n + '" title="Back up now"><i class="fa fa-download"></i></button>' +
                    '<button class="btn btn-xs btn-default" data-action="history" data-name="' + n + '" title="History, configuration and diff"><i class="fa fa-clock-o"></i></button>' +
                    '<button class="btn btn-xs btn-danger" data-action="remove" data-name="' + n + '" title="Delete (downloads the configuration history as a zip first)"><i class="fa fa-trash-o"></i></button>' +
                '</div></td>' +
            '</tr>';
        }).join('');
    }

    async function loadDevices() {
        try {
            renderDevices(await api('GET', '/devices'));
        } catch (e) {
            $devices.innerHTML = '<tr><td colspan="9"><em>Could not load devices.</em></td></tr>';
            alertBox('warning', e.message);
        }
    }

    async function loadDrivers() {
        try {
            const drivers = await api('GET', '/drivers') || [];
            driverOptions = drivers.map(d => d.name);
            $driver.innerHTML = '<option value="" disabled selected>driver</option>' +
                driverOptions.map(n => '<option value="' + esc(n) + '">' + esc(n) + '</option>').join('');
        } catch (e) {
            alertBox('warning', 'Could not load drivers: ' + e.message);
        }
    }

    // An older rusted without /api/transports yields [] (the receiver maps
    // its 404), leaving only the server-default choice.
    async function loadTransports() {
        try {
            const list = await api('GET', '/transports');
            transportOptions = Array.isArray(list) ? list.map(String) : [];
        } catch (e) {
            transportOptions = [];
        }
        $transport.innerHTML = '<option value="" selected>transport (default)</option>' +
            transportOptions.map(t => '<option value="' + esc(t) + '">' + esc(t) + '</option>').join('');
    }

    function renderCredentials(creds) {
        // Populate the device-form credential dropdown.
        const current = $credential.value;
        $credential.innerHTML = '<option value="" disabled selected>credential</option>' +
            (creds || []).map(c => '<option value="' + esc(c.name) + '">' + esc(c.name) + '</option>').join('');
        if (current) $credential.value = current;

        // Populate the credentials table.
        if (!creds || !creds.length) {
            $credentials.innerHTML = '<tr><td colspan="6"><em>No credentials yet.</em></td></tr>';
            return;
        }
        $credentials.innerHTML = creds.map(function (c) {
            const yn = b => b ? label('yes', 'success') : label('no', 'default');
            const n = esc(c.name);
            return '<tr>' +
                '<td>' + esc(c.name) + '</td>' +
                '<td>' + esc(c.username) + '</td>' +
                '<td>' + yn(c.has_password) + '</td>' +
                '<td>' + yn(c.has_key) + '</td>' +
                '<td>' + yn(c.has_enable) + '</td>' +
                '<td><button class="btn btn-xs btn-danger" data-action="remove-cred" data-name="' + n + '">' +
                    '<i class="fa fa-trash"></i> Remove</button></td>' +
            '</tr>';
        }).join('');
    }

    async function loadCredentials() {
        try {
            const creds = await api('GET', '/credentials') || [];
            credentialOptions = creds.map(c => c.name);
            renderCredentials(creds);
        } catch (e) {
            $credentials.innerHTML = '<tr><td colspan="6"><em>Could not load credentials.</em></td></tr>';
            alertBox('warning', e.message);
        }
    }

    // Load the option lists first so the device rows' dropdowns are complete.
    async function loadAll() {
        await Promise.all([loadDrivers(), loadTransports(), loadCredentials()]);
        await loadDevices();
    }

    async function doBackup(name, btn) {
        const original = btn.innerHTML;
        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i>';
        try {
            const r = await api('POST', '/devices/' + encodeURIComponent(name) + '/backup');
            const status = (r && r.status) || 'done';
            const detail = (r && r.message) || '';
            const kind = status === 'failed' ? 'danger' : 'success';
            alertBox(kind, 'Backup of ' + name + ': ' + status + (detail ? ' — ' + detail : ''));
        } catch (e) {
            alertBox('danger', 'Backup of ' + name + ' failed: ' + e.message);
        } finally {
            btn.disabled = false;
            btn.innerHTML = original;
            loadDevices();
        }
    }

    // Fetch a zip as a Blob and hand it to the browser as a download, so the
    // caller knows whether it succeeded before doing anything destructive.
    async function downloadBlob(path, filename) {
        const res = await fetch(RUSTED.base + path, {
            method: 'GET',
            credentials: 'same-origin',
            headers: { 'Accept': 'application/zip', 'X-Requested-With': 'XMLHttpRequest' },
        });
        if (!res.ok) {
            const err = new Error('HTTP ' + res.status);
            err.status = res.status;
            throw err;
        }
        const blob = await res.blob();
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        setTimeout(() => { URL.revokeObjectURL(url); a.remove(); }, 1000);
    }

    // Enable / disable (never deletes; history is kept).
    async function doToggle(name, btn) {
        const wasEnabled = btn.getAttribute('data-enabled') === '1';
        try {
            await api('POST', '/devices/' + encodeURIComponent(name) + '/enabled', { enabled: !wasEnabled });
            alertBox('success', name + ' is now ' + (wasEnabled ? 'disabled' : 'enabled') + '.');
        } catch (e) {
            alertBox('danger', 'Could not ' + (wasEnabled ? 'disable ' : 'enable ') + name + ': ' + e.message);
        }
        loadDevices();
    }

    // Delete: the device's configuration history is downloaded as a zip
    // first, and the delete only proceeds once that has succeeded.
    async function doRemove(name) {
        if (!confirm('Delete ' + name + ' from rusted?\n\n' +
            'Its full configuration history is downloaded as a zip first; ' +
            'the device is deleted only if that download succeeds.')) return;

        const safe = String(name).replace(/[^A-Za-z0-9._-]+/g, '_');
        try {
            await downloadBlob('/devices/' + encodeURIComponent(name) + '/history/zip', 'rusted-history-' + safe + '.zip');
        } catch (e) {
            if (e.status !== 404) {
                alertBox('danger', 'Could not download the configuration archive: ' + e.message +
                    '\nThe device was NOT deleted.');
                return;
            }
            if (!confirm(name + ' has no stored configuration to archive. Delete it anyway?')) return;
        }

        try {
            await api('DELETE', '/devices/' + encodeURIComponent(name));
            alertBox('success', name + ' deleted from rusted.');
        } catch (e) {
            alertBox('danger', 'Delete of ' + name + ' failed: ' + e.message);
        }
        loadDevices();
    }

    // Inline edit: save one field. An empty value is sent as "" so optional
    // fields (group, port, transport) can be cleared.
    async function doInlineEdit(el) {
        const name = el.getAttribute('data-name');
        const field = el.getAttribute('data-field');
        let value = el.value.trim();
        if (field === 'port') {
            value = value === '' ? 0 : parseInt(value, 10);
            if (isNaN(value) || value < 0 || value > 65535) {
                alertBox('warning', 'Port must be between 1 and 65535 (empty for the default).');
                el.value = el.getAttribute('data-orig') || '';
                return;
            }
        }
        el.disabled = true;
        try {
            await api('PUT', '/devices/' + encodeURIComponent(name), { [field]: value });
            alertBox('success', name + ': ' + field + ' updated.');
        } catch (e) {
            alertBox('danger', 'Could not update ' + field + ' for ' + name + ': ' + e.message);
        }
        loadDevices();
    }

    // Row actions.
    $devices.addEventListener('click', function (e) {
        const btn = e.target.closest('button[data-action]');
        if (!btn) return;
        const name = btn.getAttribute('data-name');
        const action = btn.getAttribute('data-action');
        if (action === 'toggle') doToggle(name, btn);
        else if (action === 'backup') doBackup(name, btn);
        else if (action === 'history') window.location.href = RUSTED.device + '/' + encodeURIComponent(name);
        else if (action === 'remove') doRemove(name);
    });

    // Inline edits: "change" fires on select change, and on blur/Enter for inputs.
    $devices.addEventListener('change', function (e) {
        const el = e.target.closest('.rusted-edit');
        if (el) doInlineEdit(el);
    });

    $credentials.addEventListener('click', function (e) {
        const btn = e.target.closest('button[data-action="remove-cred"]');
        if (!btn) return;
        doRemoveCredential(btn.getAttribute('data-name'));
    });

    async function doRemoveCredential(name) {
        if (!confirm('Remove credential ' + name + '?')) return;
        try {
            await api('DELETE', '/credentials/' + encodeURIComponent(name));
            alertBox('success', 'Credential ' + name + ' removed.');
            await loadCredentials();
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Remove failed: ' + e.message);
        }
    }

    $credForm.addEventListener('submit', async function (e) {
        e.preventDefault();
        const fd = new FormData($credForm);
        const payload = {
            name: fd.get('name'),
            username: fd.get('username'),
            password: fd.get('password') || '',
            enable: fd.get('enable') || '',
        };
        try {
            await api('POST', '/credentials', payload);
            alertBox('success', 'Credential ' + payload.name + ' added.');
            $credForm.reset();
            await loadCredentials();
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Add failed: ' + e.message);
        }
    });

    document.getElementById('rusted-refresh').addEventListener('click', loadAll);

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
        const transport = fd.get('transport');
        if (transport) payload.transport = transport;
        try {
            await api('POST', '/devices', payload);
            alertBox('success', 'Device ' + payload.name + ' added.');
            $form.reset();
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Add failed: ' + e.message);
        }
    });

    loadAll();
})();
</script>
@endsection
