<div class="panel panel-default panel-condensed">
    <div class="panel-heading">
        <i class="fa fa-floppy-o"></i> <strong>Configuration Backups</strong>
        <a class="pull-right" href="{{ route('rusted.index') }}" title="Open Rusted">
            <i class="fa fa-external-link"></i>
        </a>
    </div>
    <div class="panel-body" id="rusted-do-body">
        <em>Loading&hellip;</em>
    </div>
</div>

<div id="rusted-del-backdrop" style="display:none;position:fixed;top:0;right:0;bottom:0;left:0;background:#000;z-index:1040;opacity:.5"></div>
<div id="rusted-del-modal" style="display:none;position:fixed;top:0;right:0;bottom:0;left:0;z-index:1050;overflow:auto">
    <div class="modal-dialog" role="document" style="margin-top:10%">
        <div class="modal-content">
            <div class="modal-header">
                <button type="button" class="close" id="rusted-del-close">&times;</button>
                <h4 class="modal-title">Delete <span id="rusted-del-name"></span></h4>
            </div>
            <div class="modal-body">
                <p>The device's <strong>full configuration history is downloaded first</strong> as a zip;
                    the delete proceeds only if that download succeeds.</p>
                <div class="checkbox" style="margin-top:12px">
                    <label>
                        <input type="checkbox" id="rusted-del-purge">
                        <strong>Irreversible purge:</strong> also erase every stored version of this device
                        from the backup repository's git history
                    </label>
                </div>
                <p class="text-danger" id="rusted-del-purge-warn" style="display:none">
                    Requires <code>git-filter-repo</code> on the rusted host. The repository history is
                    rewritten: even old revisions become unrecoverable and all commit hashes change
                    (recorded backup references are remapped automatically).
                </p>
            </div>
            <div class="modal-footer">
                <button type="button" class="btn btn-default" id="rusted-del-cancel">Cancel</button>
                <button type="button" class="btn btn-danger" id="rusted-del-confirm">
                    <i class="fa fa-trash-o"></i> Download &amp; delete
                </button>
            </div>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        csrf: @json(csrf_token()),
        host: @json($hostname),
    };
    const $body = document.getElementById('rusted-do-body');

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
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
        try { data = await res.json(); } catch (e) { /* */ }
        return { ok: res.ok, status: res.status, data: data };
    }

    function label(text, kind) {
        return '<span class="label label-' + kind + '">' + esc(text) + '</span>';
    }

    function statusKind(s) {
        return s === 'success' ? 'success' : (s === 'failed' ? 'danger' : (s === 'unchanged' ? 'info' : 'default'));
    }

    const dev = encodeURIComponent(RUSTED.host);
    let currentDevice = null;

    async function render() {
        $body.innerHTML = '<em>Loading&hellip;</em>';
        const d = await api('GET', '/devices/' + dev);

        if (d.status === 404) {
            return renderUnmanaged();
        }
        if (!d.ok) {
            $body.innerHTML = '<div class="text-warning">Rusted API error: ' +
                esc((d.data && d.data.error) || ('HTTP ' + d.status)) + '</div>';
            return;
        }
        renderManaged(d.data);
    }

    async function renderManaged(device) {
        currentDevice = device;
        const h = await api('GET', '/devices/' + dev + '/history');
        const runs = (h.ok && Array.isArray(h.data)) ? h.data : [];
        const last = runs[0];

        let lastHtml = '<em>no backups yet</em>';
        if (last) {
            lastHtml = label(last.status, statusKind(last.status)) +
                ' <span class="text-muted">' + esc(last.started_at) + '</span>' +
                (last.commit ? ' <code>' + esc(String(last.commit).substring(0, 8)) + '</code>' : '');
        }

        const isEnabled = (device.enabled !== false);
        const toggleIcon = 'fa-power-off';
        const toggleClass = isEnabled ? 'btn-success' : 'btn-danger';
        const toggleTitle = isEnabled
            ? 'Enabled &mdash; click to disable (stops backups, keeps history)'
            : 'Disabled &mdash; click to enable (resumes backups)';

        $body.innerHTML =
            '<table class="table table-condensed" style="margin-bottom:8px">' +
            '<tr><th style="width:120px">Driver</th><td>' + esc(device.driver) + '</td></tr>' +
            '<tr><th>Last backup</th><td>' + lastHtml + '</td></tr>' +
            '</table>' +
            '<div class="btn-group btn-group-xs" role="group">' +
            '<button class="btn btn-xs ' + toggleClass + '" id="rusted-do-toggle" title="' + toggleTitle + '"><i class="fa ' + toggleIcon + '"></i></button>' +
            '<button class="btn btn-xs btn-default" id="rusted-do-backup" title="Back up now (error shown if it fails)"><i class="fa fa-download"></i></button>' +
            '<button class="btn btn-xs btn-default" id="rusted-do-history" title="Show history / view configuration"><i class="fa fa-clock-o"></i></button>' +
            '<button class="btn btn-xs btn-danger" id="rusted-do-remove" title="Delete from rusted (downloads the full configuration history first)"><i class="fa fa-trash-o"></i></button>' +
            '</div>';

        document.getElementById('rusted-do-backup').addEventListener('click', doBackup);
        document.getElementById('rusted-do-history').addEventListener('click', () => window.location.href = @json($configUrl));
        document.getElementById('rusted-do-toggle').addEventListener('click', doToggle);
        document.getElementById('rusted-do-remove').addEventListener('click', openDeleteModal);
    }

    async function doBackup(e) {
        const btn = e.currentTarget;
        const original = btn.innerHTML;
        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Backing up&hellip;';
        const r = await api('POST', '/devices/' + dev + '/backup');
        btn.disabled = false;
        btn.innerHTML = original;
        if (!r.ok) {
            alert('Backup failed: ' + ((r.data && r.data.error) || ('HTTP ' + r.status)));
        }
        render();
    }

    // Fetch a binary response (the history zip) as a Blob and trigger a download
    // without navigating away from the page, so removal can follow it server-side.
    async function downloadBlob(path, filename) {
        const res = await fetch(RUSTED.base + path, {
            method: 'GET',
            credentials: 'same-origin',
            headers: {
                'Accept': 'application/zip',
                'X-Requested-With': 'XMLHttpRequest',
                'X-CSRF-TOKEN': RUSTED.csrf,
            },
        });
        if (!res.ok) {
            throw new Error('HTTP ' + res.status);
        }
        const blob = await res.blob();
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        setTimeout(() => { URL.revokeObjectURL(url); document.body.removeChild(a); }, 1000);
    }

    // Enable / disable (never delete) — a soft toggle that preserves the device's
    // configuration history in rusted. Mirrors the button's "isEnabled" rule so
    // a device with no explicit flag (treated as enabled) toggles to disabled.
    async function doToggle() {
        if (!currentDevice) return;
        const isNowEnabled = (currentDevice.enabled !== false);
        const r = await api('POST', '/devices/' + dev + '/enabled', { enabled: !isNowEnabled });
        if (!r.ok) {
            alert('Toggle failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
            return;
        }
        render();
    }

    // Remove = a real delete from rusted. Since this erases the device, a full
    // archive of every configuration revision is downloaded first (so it can be
    // re-imported later if needed); an optional checkbox purges the device's
    // configurations from the backup git history irreversibly.
    async function doRemove(purge) {
        if (!currentDevice) return;

        const filename = 'rusted-history-' + RUSTED.host + '.zip';
        try {
            await downloadBlob('/devices/' + dev + '/history/zip', filename);
        } catch (e) {
            alert('Could not download the configuration archive: ' + e.message +
                '\n\nThe device was NOT deleted. Fix this and try again, or use the rusted CLI.');
            return;
        }

        const r = await api('DELETE', '/devices/' + dev + (purge ? '?purge=true' : ''));
        if (!r.ok) {
            alert('Delete failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)) +
                '\n\nThe archive was saved, but the device could not be removed.');
            return;
        }
        render();
    }

    function openDeleteModal() {
        if (!currentDevice) return;
        document.getElementById('rusted-del-name').textContent = currentDevice.name || RUSTED.host;
        document.getElementById('rusted-del-purge').checked = false;
        document.getElementById('rusted-del-purge-warn').style.display = 'none';
        document.getElementById('rusted-del-modal').style.display = 'block';
        document.getElementById('rusted-del-backdrop').style.display = 'block';
    }
    function closeDeleteModal() {
        document.getElementById('rusted-del-modal').style.display = 'none';
        document.getElementById('rusted-del-backdrop').style.display = 'none';
    }

    document.getElementById('rusted-del-purge').addEventListener('change', function () {
        document.getElementById('rusted-del-purge-warn').style.display = this.checked ? 'block' : 'none';
    });
    document.getElementById('rusted-del-cancel').addEventListener('click', closeDeleteModal);
    document.getElementById('rusted-del-close').addEventListener('click', closeDeleteModal);
    document.getElementById('rusted-del-backdrop').addEventListener('click', closeDeleteModal);
    document.getElementById('rusted-del-confirm').addEventListener('click', function () {
        const purge = document.getElementById('rusted-del-purge').checked;
        closeDeleteModal();
        doRemove(purge);
    });

    async function renderUnmanaged() {
        const drv = await api('GET', '/drivers');
        const cred = await api('GET', '/credentials');
        const drivers = (drv.ok && Array.isArray(drv.data)) ? drv.data : [];
        const creds = (cred.ok && Array.isArray(cred.data)) ? cred.data : [];

        const driverOpts = drivers.map(d => '<option value="' + esc(d.name) + '">' + esc(d.name) + '</option>').join('');
        const credOpts = creds.map(c => '<option value="' + esc(c.name) + '">' + esc(c.name) + ' (' + esc(c.username) + ')</option>').join('');

        $body.innerHTML =
            '<p class="text-muted">This device is not managed by rusted.</p>' +
            '<div class="form-group"><select id="rusted-do-driver" class="form-control input-sm">' +
                '<option value="" disabled selected>driver</option>' + driverOpts + '</select></div>' +
            '<div class="form-group"><select id="rusted-do-cred" class="form-control input-sm">' +
                (creds.length ? credOpts : '<option value="" disabled selected>no credentials — add one in Rusted</option>') +
            '</select></div>' +
            '<button class="btn btn-xs btn-success" id="rusted-do-add" ' + (creds.length ? '' : 'disabled') + '>' +
                '<i class="fa fa-plus"></i> Add to rusted</button>';

        const addBtn = document.getElementById('rusted-do-add');
        if (addBtn) addBtn.addEventListener('click', doAdd);
    }

    async function doAdd() {
        const driver = document.getElementById('rusted-do-driver').value;
        const credential = document.getElementById('rusted-do-cred').value;
        if (!driver || !credential) {
            alert('Pick a driver and credential.');
            return;
        }
        const r = await api('POST', '/devices', {
            name: RUSTED.host, host: RUSTED.host, driver: driver, credential: credential,
        });
        if (!r.ok) {
            alert('Add failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
            return;
        }
        render();
    }

    render();
})();
</script>
