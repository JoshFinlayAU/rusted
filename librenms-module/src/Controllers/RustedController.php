<?php

namespace AthenaNetworks\RustedLibrenms\Controllers;

use App\Models\Device;
use AthenaNetworks\RustedLibrenms\Support\RustedClient;
use Illuminate\Contracts\View\View;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Illuminate\Routing\Controller;
use ZipArchive;

class RustedController extends Controller
{
    /** Render the page shell; all data is loaded by the browser via AJAX. */
    public function index(): View
    {
        return view('rusted::page', ['title' => 'Rusted Backups']);
    }

    // --- AJAX/JSON receiver -------------------------------------------------
    // These are same-origin routes the browser calls. Each relays to the rusted
    // API server-side and forwards rusted's JSON body and status code.

    public function apiDevices(): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/devices'));
    }

    public function apiDrivers(): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/drivers'));
    }

    public function apiTransports(): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/transports'));
    }

    public function apiCredentials(): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/credentials'));
    }

    public function apiAddCredential(Request $request): JsonResponse
    {
        $data = $request->validate([
            'name' => 'required|string',
            'username' => 'required|string',
            'password' => 'nullable|string',
            'enable' => 'nullable|string',
        ]);

        return $this->proxy(fn () => RustedClient::client()->post('/api/credentials', $data));
    }

    public function apiRemoveCredential(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->delete('/api/credentials/'.rawurlencode($name)));
    }

    public function apiDevice(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/devices/'.rawurlencode($name)));
    }

    public function apiAddDevice(Request $request): JsonResponse
    {
        $data = $request->validate([
            'name' => 'required|string',
            'host' => 'required|string',
            'port' => 'nullable|integer',
            'driver' => 'required|string',
            'credential' => 'required|string',
            'group' => 'nullable|string',
            'transport' => 'nullable|string',
        ]);

        // Cast port to int: the browser sends a string from <input type="number">,
        // and Go's json.Decoder rejects JSON string → int.
        if (isset($data['port'])) {
            $data['port'] = (int) $data['port'];
        }

        return $this->proxy(fn () => RustedClient::client()->post('/api/devices', $data));
    }

    /**
     * Delete a device from rusted. The optional ?purge=true flag MUST be
     * relayed explicitly: the rusted API reads it from the query string of
     * the proxied call, so dropping it here would silently downgrade an
     * irreversible purge to a plain delete.
     */
    public function apiRemoveDevice(string $name, Request $request): JsonResponse
    {
        $suffix = $request->boolean('purge') ? '?purge=true' : '';

        return $this->proxy(fn () => RustedClient::client()->delete('/api/devices/'.rawurlencode($name).$suffix));
    }

    /**
     * Flip a device's `enabled` flag (enable/disable). The device is never
     * deleted: disabling stops backups while preserving its configuration
     * history; enabling resumes them. All identity/group/transport/credential
     * fields are preserved — only `enabled` is changed, via a rusted upsert.
     *
     * Body: {"enabled": true|false}
     */
    public function apiSetEnabled(string $name, Request $request): JsonResponse
    {
        $enabled = $request->validate([
            'enabled' => 'required|boolean',
        ])['enabled'];

        try {
            $g = RustedClient::client()->get('/api/devices/'.rawurlencode($name));
        } catch (\Throwable $e) {
            return response()->json(['error' => 'Could not reach the rusted API: '.$e->getMessage()], 502);
        }

        if (! $g->successful()) {
            return response()->json($g->json() ?? ['error' => 'HTTP '.$g->status()], $g->status() ?: 502);
        }

        $e = $g->json();
        $payload = [
            'name'       => $e['name'] ?? $name,
            'host'       => $e['host'] ?? $name,
            'port'       => $e['port'] ?? 0,
            'driver'     => $e['driver'] ?? '',
            'transport'  => $e['transport'] ?? '',
            'credential' => $e['credential'] ?? '',
            'group'      => $e['group'] ?? '',
            'enabled'    => $enabled,
        ];

        try {
            $r = RustedClient::client()->post('/api/devices', $payload);
        } catch (\Throwable $ex) {
            return response()->json(['error' => 'Could not reach the rusted API: '.$ex->getMessage()], 502);
        }

        return $this->proxy(fn () => $r);
    }

    /**
     * Partially update a device (driver, port, transport, etc.) via rusted's
     * upsert POST endpoint.  The browser sends only the fields to change; this
     * method fetches the current device, merges in the patches, and POSTs the
     * full record back so rusted upserts in place.
     *
     * Body: any subset of {"driver","port","transport","host","group","credential"}
     */
    public function apiUpdateDevice(string $name, Request $request): JsonResponse
    {
        $data = $request->validate([
            'driver'     => 'nullable|string',
            'port'       => 'nullable|integer',
            'transport'  => 'nullable|string',
            'host'       => 'nullable|string',
            'group'      => 'nullable|string',
            'credential' => 'nullable|string',
        ]);

        try {
            $g = RustedClient::client()->get('/api/devices/'.rawurlencode($name));
        } catch (\Throwable $e) {
            return response()->json(['error' => 'Could not reach the rusted API: '.$e->getMessage()], 502);
        }

        if (! $g->successful()) {
            return response()->json($g->json() ?? ['error' => 'HTTP '.$g->status()], $g->status() ?: 502);
        }

        $e = $g->json();
        // Cast port to int: the browser sends a string from <input type="number">,
        // and Go's json.Decoder rejects JSON string → int.
        $port = $data['port'] ?? null;
        if ($port !== null) {
            $port = (int) $data['port'];
        }
        $payload = [
            'name'       => $e['name'] ?? $name,
            'host'       => $data['host'] ?? ($e['host'] ?? $name),
            'port'       => $port ?? ($e['port'] ?? 0),
            'driver'     => $data['driver'] ?? ($e['driver'] ?? ''),
            'transport'  => $data['transport'] ?? ($e['transport'] ?? ''),
            'credential' => $data['credential'] ?? ($e['credential'] ?? ''),
            'group'      => $data['group'] ?? ($e['group'] ?? ''),
            'enabled'    => $e['enabled'] ?? true,
        ];

        try {
            $r = RustedClient::client()->post('/api/devices', $payload);
        } catch (\Throwable $ex) {
            return response()->json(['error' => 'Could not reach the rusted API: '.$ex->getMessage()], 502);
        }

        return $this->proxy(fn () => $r);
    }

    public function apiBackup(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->post('/api/devices/'.rawurlencode($name).'/backup'));
    }

    public function apiHistory(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/history'));
    }

    /**
     * Fetch a device's stored configuration, optionally at a given git commit.
     * Forwards the optional ?commit=<hash> query parameter to rusted and returns
     * the raw text/plain body (never JSON-encoded).
     */
    public function apiConfig(string $name, Request $request): Response
    {
        return $this->proxyText(function () use ($name, $request) {
            $uri = '/api/devices/'.rawurlencode($name).'/config';
            if ($commit = $request->query('commit')) {
                $uri .= '?commit='.rawurlencode($commit);
            }

            return RustedClient::client()->get($uri);
        });
    }

    /** List the git revisions of a device's config (newest first). JSON. */
    public function apiVersions(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/versions'));
    }

    /**
     * Diff two revisions (or a single revision against its parent / HEAD).
     * Forwards ?from=<hash>&to=<hash> to rusted and returns the unified diff as
     * raw text/plain.
     */
    public function apiDiff(string $name, Request $request): Response
    {
        return $this->proxyText(function () use ($name, $request) {
            $uri = '/api/devices/'.rawurlencode($name).'/diff';
            $qs = [];
            foreach (['from', 'to'] as $key) {
                if ($value = $request->query($key)) {
                    $qs[] = rawurlencode($key).'='.rawurlencode($value);
                }
            }
            if ($qs) {
                $uri .= '?'.implode('&', $qs);
            }

            return RustedClient::client()->get($uri);
        });
    }

    /** Render the per-device configuration viewer page. */
    public function device(string $name): View
    {
        return view('rusted::device', ['hostname' => $name]);
    }

    /**
     * Execute a rusted API call that returns a text body and forward its raw body
     * and status, preserving the content type. Turns any transport failure into a
     * clean 502 so the browser always gets a text response.
     *
     * @param  callable(): \Illuminate\Http\Client\Response  $call
     */
    private function proxyText(callable $call): Response
    {
        try {
            $resp = $call();
        } catch (\Throwable $e) {
            return response('Could not reach the rusted API: '.$e->getMessage(), 502)
                ->header('Content-Type', 'text/plain; charset=utf-8');
        }

        return response($resp->body(), $resp->status() ?: 502)
            ->header('Content-Type', $resp->header('Content-Type') ?: 'text/plain; charset=utf-8');
    }

    /**
     * Execute a rusted API call and forward its JSON + status, turning any
     * transport failure into a clean 502 so the browser always gets JSON.
     *
     * @param  callable(): \Illuminate\Http\Client\Response  $call
     */
    private function proxy(callable $call): JsonResponse
    {
        try {
            $resp = $call();
        } catch (\Throwable $e) {
            return response()->json(
                ['error' => 'Could not reach the rusted API: '.$e->getMessage()],
                502
            );
        }

        $body = $resp->json();
        if (! is_array($body)) {
            $body = ['raw' => $resp->body()];
        }

        return response()->json($body, $resp->status() ?: 502);
    }

    /** Render the bulk-sync page shell. */
    public function sync(): View
    {
        return view('rusted::sync');
    }

    /**
     * Compute the two-pane sync state by joining LibreNMS devices against the
     * rusted device inventory (matched on LibreNMS hostname == rusted device
     * name, case-insensitively):
     *  - left:  LibreNMS devices not actively backed up by rusted (not in rusted,
     *           or disabled in rusted). `rust_state` tells them apart.
     *  - right: rusted devices that are enabled AND also in LibreNMS (selectable
     *           to disable / remove).
     *  - grey:  rusted devices that are NOT in LibreNMS (shown greyed, locked).
     */
    public function syncState(): JsonResponse
    {
        $libre = Device::query()
            ->whereNotNull('hostname')
            ->where('hostname', '!=', '')
            ->select(['hostname', 'sysName', 'os', 'ip', 'status', 'disabled'])
            ->orderBy('hostname')
            ->get();

        $resp = RustedClient::client()->get('/api/devices');
        if (! $resp->successful()) {
            return response()->json(
                ['error' => 'Could not reach the rusted API: '.$resp->body()],
                $resp->status() ?: 502
            );
        }

        $rusted = is_array($resp->json()) ? $resp->json() : [];

        // In-memory set of LibreNMS hostnames for O(1) membership tests.
        $libreNames = [];
        foreach ($libre as $d) {
            if (! empty($d->hostname)) {
                $libreNames[strtolower($d->hostname)] = true;
            }
        }

        // Partitions of the rusted name set.
        $enabledRusted = [];   // enabled rusted devices (in rusted + LibreNMS => "right")
        $disabledRusted = [];  // disabled rusted devices (in rusted + LibreNMS => still "left")
        $right = [];
        $grey = [];

        foreach ($rusted as $r) {
            if (empty($r['name'])) {
                continue;
            }
            $low = strtolower($r['name']);
            $entry = [
                'name'    => $r['name'],
                'host'    => $r['host'] ?? $r['name'],
                'driver'  => $r['driver'] ?? '',
                'group'   => $r['group'] ?? '',
                'enabled' => (bool) ($r['enabled'] ?? true),
            ];
            if (! isset($libreNames[$low])) {
                $grey[] = $entry;                // rusted but not in LibreNMS -> locked
                continue;
            }
            if (! empty($entry['enabled'])) {
                $enabledRusted[$low] = true;
                $right[] = $entry;              // actively managed -> selectable
            } else {
                $disabledRusted[$low] = true;   // disabled but still in rusted -> left, recoverable
            }
        }

        $left = [];
        foreach ($libre as $d) {
            $low = strtolower($d->hostname);
            if (isset($enabledRusted[$low])) {
                continue;                       // actively managed -> belongs in right
            }
            $left[] = [
                'name'      => $d->hostname,
                'host'      => $d->hostname,
                'os'        => $d->os,
                'sysName'   => $d->sysName,
                'status'    => (bool) $d->status,
                'disabled'  => (bool) $d->disabled,
                'rust_state'=> isset($disabledRusted[$low]) ? 'disabled' : 'absent',
            ];
        }

        return response()->json([
            'left'  => $left,
            'right' => $right,
            'grey'  => $grey,
        ]);
    }

    /**
     * Add selected LibreNMS devices to rusted in one batch, using a single
     * driver + credential (per family). For devices already present in rusted
     * (disabled), their group/transport are preserved while re-enabling them.
     */
    public function syncAdd(Request $request): JsonResponse
    {
        $data = $request->validate([
            'driver'     => 'required|string',
            'credential' => 'required|string',
            'devices'    => 'required|array|min:1',
            'devices.*'  => 'required|string',
        ]);

        $added = [];
        $failed = [];

        foreach ($data['devices'] as $host) {
            $payload = [
                'name'       => $host,
                'host'       => $host,
                'port'       => 0,
                'driver'     => $data['driver'],
                'transport'  => '',
                'credential' => $data['credential'],
                'group'      => '',
                'enabled'    => true,
            ];

            // If the device already exists in rusted (e.g. disabled), preserve
            // its group + transport so re-enabling doesn't clobber them.
            try {
                $existing = RustedClient::client()->get('/api/devices/'.rawurlencode($host));
                if ($existing->successful() && is_array($e = $existing->json())) {
                    $payload['group'] = $e['group'] ?? '';
                    $payload['transport'] = $e['transport'] ?? '';
                    $payload['port'] = $e['port'] ?? 0;
                }
            } catch (\Throwable) {
                // not present -> genuinely new; defaults are fine
            }

            try {
                $r = RustedClient::client()->post('/api/devices', $payload);
            } catch (\Throwable $e) {
                $failed[] = ['name' => $host, 'error' => $e->getMessage()];
                continue;
            }
            if ($r->successful()) {
                $added[] = $host;
            } else {
                $failed[] = ['name' => $host, 'error' => $r->json('error') ?? ('HTTP '.$r->status())];
            }
        }

        return response()->json(['added' => $added, 'failed' => $failed]);
    }

    /**
     * "Remove" selected devices from the right pane by *disabling* them in
     * rusted (never deleting), so their configuration history is preserved on
     * disk/git. Disabled devices drop back to the left pane (rust_state=disabled)
     * and can be re-enabled later with Add. Devices that fail to disable are
     * returned in `kept` and stay on the right, highlighted red.
     */
    public function syncDisable(Request $request): JsonResponse
    {
        $data = $request->validate([
            'devices'   => 'required|array|min:1',
            'devices.*' => 'required|string',
        ]);

        $disabled = [];
        $kept = [];

        foreach ($data['devices'] as $host) {
            // Fetch current device so the upsert preserves name/host/driver/
            // transport/credential/group, flipping only enabled -> false.
            try {
                $g = RustedClient::client()->get('/api/devices/'.rawurlencode($host));
            } catch (\Throwable $e) {
                $kept[] = ['name' => $host, 'error' => $e->getMessage()];
                continue;
            }
            if (! $g->successful()) {
                $kept[] = ['name' => $host, 'error' => $g->json('error') ?? ('HTTP '.$g->status())];
                continue;
            }
            $e = $g->json();
            $payload = [
                'name'       => $e['name'] ?? $host,
                'host'       => $e['host'] ?? $host,
                'port'       => $e['port'] ?? 0,
                'driver'     => $e['driver'] ?? '',
                'transport'  => $e['transport'] ?? '',
                'credential' => $e['credential'] ?? '',
                'group'      => $e['group'] ?? '',
                'enabled'    => false,
            ];
            try {
                $r = RustedClient::client()->post('/api/devices', $payload);
            } catch (\Throwable $ex) {
                $kept[] = ['name' => $host, 'error' => $ex->getMessage()];
                continue;
            }
            if ($r->successful()) {
                $disabled[] = $host;
            } else {
                $kept[] = ['name' => $host, 'error' => $r->json('error') ?? ('HTTP '.$r->status())];
            }
        }

        return response()->json(['disabled' => $disabled, 'kept' => $kept]);
    }

    /**
     * Download EVERY device's full configuration history (every git revision +
     * the latest config of each) as one zip — the "massive" export used to
     * archive or migrate all configurations at once. Each device lands in its
     * own folder with a MANIFEST.txt carrying the git metadata of every stored
     * version; a top-level MANIFEST.txt indexes what is inside.
     */
    public function apiExportZip(): Response
    {
        try {
            $resp = RustedClient::client()->get('/api/devices');
        } catch (\Throwable $e) {
            return response('Could not reach the rusted API: '.$e->getMessage(), 502)
                ->header('Content-Type', 'text/plain; charset=utf-8');
        }
        if (! $resp->successful()) {
            return response('Could not list devices: HTTP '.$resp->status(), 502)
                ->header('Content-Type', 'text/plain; charset=utf-8');
        }

        $devices = is_array($resp->json()) ? $resp->json() : [];
        $path = tempnam(sys_get_temp_dir(), 'rusted-export-');
        $zip = new ZipArchive();
        if ($zip->open($path, ZipArchive::CREATE | ZipArchive::OVERWRITE) !== true) {
            @unlink($path);
            return response('Could not create the export archive.', 500)
                ->header('Content-Type', 'text/plain; charset=utf-8');
        }

        $index = [];   // name => ['revisions' => int, 'captured_at' => string]
        foreach ($devices as $d) {
            $name = $d['name'] ?? '';
            if ($name === '') {
                continue;
            }
            $got = $this->snapshotDevice($zip, $name);
            if ($got > 0) {
                $index[$name] = [
                    'revisions'   => $got,
                    'captured_at' => $this->lastCapturedAt($name),
                ];
            }
        }
        $zip->addFromString('MANIFEST.txt', $this->buildExportManifest($index));
        $zip->close();

        if (empty($index) || filesize($path) <= 0) {
            @unlink($path);
            return response('No configuration history found for any device.', 404)
                ->header('Content-Type', 'text/plain; charset=utf-8');
        }

        $content = file_get_contents($path);
        @unlink($path);

        return response($content, 200, [
            'Content-Type'        => 'application/zip',
            'Content-Disposition' => 'attachment; filename="rusted-configs-'.date('Ymd-His').'.zip"',
        ]);
    }

    /**
     * Download a device's full configuration history (every git revision +
     * the latest config) as a zip, so it can be archived before the device is
     * ever removed from rusted. A MANIFEST.txt inside states exactly when each
     * version was captured (git commit date/time), by whom and why.
     */
    public function apiHistoryZip(string $name): Response
    {
        $path = $this->captureHistoryZip($name);
        if ($path === null) {
            return response('No configuration history for this device.', 404)
                ->header('Content-Type', 'text/plain; charset=utf-8');
        }

        $content = file_get_contents($path);
        @unlink($path);

        return response($content, 200, [
            'Content-Type'        => 'application/zip',
            'Content-Disposition' => 'attachment; filename="rusted-history-'.$name.'.zip"',
        ]);
    }

    /**
     * Write every revision (and the latest on-disk config) of a device into a
     * temp zip and return its path, or null if there is no history yet.
     */
    private function captureHistoryZip(string $name): ?string
    {
        $path = tempnam(sys_get_temp_dir(), 'rusted-history-');
        $zip = new ZipArchive();
        if ($zip->open($path, ZipArchive::CREATE | ZipArchive::OVERWRITE) !== true) {
            @unlink($path);
            return null;
        }

        $got = $this->snapshotDevice($zip, $name);
        $zip->close();

        if ($got <= 0 || filesize($path) <= 0) {
            @unlink($path);
            return null;
        }

        return $path;
    }

    /**
     * Write every git revision (and the latest config) of a device into $zip,
     * plus a MANIFEST.txt describing each version with its git metadata.
     * Returns the number of files captured (0 = nothing to export).
     */
    private function snapshotDevice(ZipArchive $zip, string $name): int
    {
        $got = 0;
        try {
            $v = RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/versions');
            $versions = $v->successful() && is_array($v->json()) ? $v->json() : [];
        } catch (\Throwable) {
            $versions = [];
        }

        // Latest on disk.
        $hasLatest = false;
        try {
            $latest = RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/config');
            if ($latest->successful() && $latest->body() !== '') {
                $zip->addFromString($name.'/latest.cfg', $latest->body());
                $hasLatest = true;
                $got++;
            }
        } catch (\Throwable) {
            // device may have no backup yet
        }

        $seen = [];
        foreach ($versions as $ver) {
            $commit = $ver['commit'] ?? '';
            if ($commit === '' || isset($seen[$commit])) {
                continue;
            }
            try {
                $c = RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/config?commit='.rawurlencode($commit));
                if ($c->successful()) {
                    $zip->addFromString($name.'/'.$commit.'.cfg', $c->body());
                    $seen[$commit] = true;
                    $got++;
                }
            } catch (\Throwable) {
                continue;
            }
        }

        if ($got > 0) {
            $zip->addFromString($name.'/MANIFEST.txt', $this->buildManifest($name, $versions, $hasLatest));
        }

        return $got;
    }

    /**
     * RFC3339 timestamp of the newest stored version of a device, or '-'.
     * Used for the summary table of the bulk export.
     */
    private function lastCapturedAt(string $name): string
    {
        try {
            $v = RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/versions');
            $versions = $v->successful() && is_array($v->json()) ? $v->json() : [];
        } catch (\Throwable) {
            return '-';
        }

        return $versions[0]['captured_at'] ?? '-';
    }

    /**
     * Human-readable index of every version in a device folder: when each
     * configuration was captured (git author date), by whom, and the commit
     * subject — so <commit>.cfg files can be traced back to their moment.
     *
     * @param  array<int, array<string, mixed>>  $versions  rusted /versions entries
     */
    private function buildManifest(string $name, array $versions, bool $hasLatest): string
    {
        $out = "rusted configuration history\n"
            ."============================\n"
            .'device     : '.$name."\n"
            .'generated  : '.date('c')."\n"
            .'revisions  : '.count($versions)."\n"
            ."source     : rusted backup repository (git)\n"
            ."\n"
            ."Files\n"
            ."-----\n"
            ."  <commit>.cfg  one stored revision of the device configuration\n";
        if ($hasLatest) {
            $out .= "  latest.cfg    the most recent on-disk copy at export time\n";
        }
        $out .= "\n"
            ."Version index (newest first)\n"
            ."----------------------------\n";

        if (! $versions) {
            $out .= "(no git revisions recorded)\n";

            return $out;
        }

        // commit(8) captured_at(26) author(16) subject...
        $out .= str_pad('commit', 8).' '.str_pad('captured_at', 26).' '
            .str_pad('author', 16)."subject\n";
        foreach ($versions as $ver) {
            $out .= str_pad(substr((string) ($ver['commit'] ?? ''), 0, 7), 8).' '
                .str_pad(substr((string) ($ver['captured_at'] ?? '-'), 0, 25), 26).' '
                .str_pad(substr((string) ($ver['author'] ?? '-'), 0, 15), 16)
                .((string) ($ver['subject'] ?? ''))."\n";
        }

        return $out;
    }

    /**
     * Top-level MANIFEST.txt of the bulk export: which devices are included,
     * how many versions each carries and when the newest one was captured.
     *
     * @param  array<string, array{revisions: int, captured_at: string}>  $index
     */
    private function buildExportManifest(array $index): string
    {
        $out = "rusted bulk configuration export\n"
            ."================================\n"
            .'generated : '.date('c')."\n"
            .'devices   : '.count($index)."\n"
            ."\n"
            ."Each sub-folder holds one device's full history plus its own\n"
            ."MANIFEST.txt listing the git metadata of every stored version.\n"
            ."\n"
            ."device                            revisions  last capture\n";
        foreach ($index as $name => $info) {
            $out .= str_pad(substr($name, 0, 31), 32).' '
                .str_pad((string) $info['revisions'], 9).' '
                .$info['captured_at']."\n";
        }

        return $out;
    }
}
