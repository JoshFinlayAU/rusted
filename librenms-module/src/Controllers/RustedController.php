<?php

namespace AthenaNetworks\RustedLibrenms\Controllers;

use AthenaNetworks\RustedLibrenms\Support\RustedClient;
use Illuminate\Contracts\View\View;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
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
