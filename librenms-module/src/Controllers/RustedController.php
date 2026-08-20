<?php

namespace AthenaNetworks\RustedLibrenms\Controllers;

use AthenaNetworks\RustedLibrenms\Support\RustedClient;
use Illuminate\Contracts\View\View;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Illuminate\Routing\Controller;

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
