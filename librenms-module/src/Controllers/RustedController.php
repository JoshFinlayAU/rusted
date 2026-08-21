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
        ]);

        return $this->proxy(fn () => RustedClient::client()->post('/api/devices', $data));
    }

    public function apiRemoveDevice(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->delete('/api/devices/'.rawurlencode($name)));
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
     * Compute the two-pane sync state by joining LibreNMS devices against the
     * rusted device inventory (matched on LibreNMS hostname == rusted device
     * name, case-insensitively):
     *  - left:  LibreNMS devices not yet in rusted (candidates to add)
     *  - right: rusted devices that are also in LibreNMS (selectable to remove)
     *  - grey:  rusted devices that are NOT in LibreNMS (shown greyed, locked)
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

        // rusted device names (lower-cased) for set membership tests.
        $rustNames = [];
        foreach ($rusted as $r) {
            if (! empty($r['name'])) {
                $rustNames[strtolower($r['name'])] = true;
            }
        }

        // In-memory set of LibreNMS hostnames for O(1) membership tests.
        $libreNames = [];
        foreach ($libre as $d) {
            if (! empty($d->hostname)) {
                $libreNames[strtolower($d->hostname)] = true;
            }
        }

        $left = [];   // LibreNMS devices not yet in rusted (candidates to add)
        $right = [];  // rusted devices also in LibreNMS (selectable to remove)
        $grey = [];   // rusted devices NOT in LibreNMS (shown greyed, locked)

        foreach ($libre as $d) {
            if (isset($rustNames[strtolower($d->hostname)])) {
                continue; // already managed by rusted
            }
            $left[] = [
                'name'      => $d->hostname,
                'host'      => $d->hostname,
                'os'        => $d->os,
                'sysName'   => $d->sysName,
                'status'    => (bool) $d->status,
                'disabled'  => (bool) $d->disabled,
            ];
        }

        foreach ($rusted as $r) {
            if (empty($r['name'])) {
                continue;
            }
            $entry = [
                'name'    => $r['name'],
                'host'    => $r['host'] ?? $r['name'],
                'driver'  => $r['driver'] ?? '',
                'group'   => $r['group'] ?? '',
                'enabled' => (bool) ($r['enabled'] ?? true),
            ];
            // A LibreNMS device is identified by hostname; match on that.
            if (isset($libreNames[strtolower($r['name'])])) {
                $right[] = $entry; // in rusted AND in LibreNMS → selectable for removal
            } else {
                $grey[] = $entry;  // in rusted but NOT in LibreNMS → locked
            }
        }

        return response()->json([
            'left'  => $left,
            'right' => $right,
            'grey'  => $grey,
        ]);
    }

    /**
     * Add the selected LibreNMS devices to rusted using a single driver +
     * credential (chosen by the user, per-family). Returns which were added and
     * which failed (failed ones must stay on the left, highlighted red).
     */
    public function syncAdd(Request $request): JsonResponse
    {
        $data = $request->validate([
            'driver'    => 'required|string',
            'credential'=> 'required|string',
            'devices'   => 'required|array|min:1',
            'devices.*' => 'required|string',
        ]);

        $added = [];
        $failed = [];

        foreach ($data['devices'] as $host) {
            $payload = [
                'name'       => $host,
                'host'       => $host,
                'port'       => 0,            // rusted defaults to 22
                'driver'     => $data['driver'],
                'transport'  => '',
                'credential' => $data['credential'],
                'group'      => '',
                'enabled'    => true,
            ];
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
     * Remove the selected rusted devices. To guard against accidental removal,
     * every revision's configuration is snapshotted into a zip *before* the
     * rusted DELETE, and the zip is handed back as a one-shot download token.
     * Devices whose delete fails are returned in `kept` (and stay on the right).
     */
    public function syncRemove(Request $request): JsonResponse
    {
        $data = $request->validate([
            'devices'   => 'required|array|min:1',
            'devices.*' => 'required|string',
        ]);

        $dir = storage_path('app/rusted-history');
        if (! is_dir($dir)) {
            mkdir($dir, 0o755, true);
        }

        $token = bin2hex(random_bytes(8));
        $zipPath = $dir.'/'.$token.'.zip';
        $zip = new ZipArchive();
        if ($zip->open($zipPath, ZipArchive::CREATE | ZipArchive::OVERWRITE) !== true) {
            return response()->json(['error' => 'Could not create history snapshot'], 500);
        }

        $removed = [];
        $kept = [];
        $hadHistory = false;

        foreach ($data['devices'] as $host) {
            $name = $host;

            // Snapshot history first: every version + the latest config.
            $hadAny = $this->snapshotDevice($zip, $name, $hadHistory);
            $hadHistory = $hadHistory || $hadAny;

            // Then delete from rusted.
            try {
                $r = RustedClient::client()->delete('/api/devices/'.rawurlencode($name));
            } catch (\Throwable $e) {
                $kept[] = ['name' => $name, 'error' => $e->getMessage()];
                continue;
            }
            if ($r->successful()) {
                $removed[] = $name;
            } else {
                $kept[] = ['name' => $name, 'error' => $r->json('error') ?? ('HTTP '.$r->status())];
            }
        }

        $zip->close();
        if (! $hadHistory) {
            @unlink($zipPath);
            $token = null;
        }

        return response()->json([
            'removed' => $removed,
            'kept'    => $kept,
            'token'   => $token,
        ]);
    }

    /**
     * Stream a previously captured history snapshot to the browser, then delete
     * the temporary file so it cannot be re-downloaded.
     */
    public function syncHistoryDownload(string $token): Response
    {
        $path = storage_path('app/rusted-history/'.$token.'.zip');
        if (! is_file($path)) {
            abort(404, 'snapshot not found or already downloaded');
        }

        $content = file_get_contents($path);
        @unlink($path);

        return response($content, 200, [
            'Content-Type'        => 'application/zip',
            'Content-Disposition' => 'attachment; filename="rusted-history-'.$token.'.zip"',
        ]);
    }

    /** Render the bulk-sync page shell. */
    public function sync(): View
    {
        return view('rusted::sync');
    }

    /**
     * Write every git revision (and the latest config) of a device into $zip.
     * Returns true if anything was captured.
     */
    private function snapshotDevice(ZipArchive $zip, string $name, bool &$hadHistory): bool
    {
        $captured = false;
        try {
            $v = RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/versions');
            $versions = $v->successful() && is_array($v->json()) ? $v->json() : [];
        } catch (\Throwable) {
            $versions = [];
        }

        // Latest on disk.
        try {
            $latest = RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/config');
            if ($latest->successful() && $latest->body() !== '') {
                $zip->addFromString($name.'/latest.cfg', $latest->body());
                $captured = true;
            }
        } catch (\Throwable) {
            // device may have no backup yet
        }

        foreach ($versions as $ver) {
            $commit = $ver['commit'] ?? '';
            if ($commit === '') {
                continue;
            }
            try {
                $c = RustedClient::client()->get('/api/devices/'.rawurlencode($name).'/config?commit='.rawurlencode($commit));
                if ($c->successful()) {
                    $zip->addFromString($name.'/'.$commit.'.cfg', $c->body());
                    $captured = true;
                }
            } catch (\Throwable) {
                continue;
            }
        }

        return $captured;
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
}
