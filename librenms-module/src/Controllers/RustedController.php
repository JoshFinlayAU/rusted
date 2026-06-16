<?php

namespace AthenaNetworks\RustedLibrenms\Controllers;

use AthenaNetworks\RustedLibrenms\Support\RustedClient;
use Illuminate\Contracts\View\View;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\Routing\Controller;

class RustedController extends Controller
{
    public function index(): View
    {
        $devices = [];
        $drivers = [];
        $error = null;
        try {
            $devices = RustedClient::devices();
            $drivers = RustedClient::drivers();
        } catch (\Throwable $e) {
            $error = 'Could not reach the rusted API: '.$e->getMessage();
        }

        return view('rusted::page', [
            'title' => 'Rusted Backups',
            'devices' => $devices,
            'drivers' => $drivers,
            'apiError' => $error,
        ]);
    }

    public function store(Request $request): RedirectResponse
    {
        $data = $request->validate([
            'name' => 'required|string',
            'host' => 'required|string',
            'port' => 'nullable|integer',
            'driver' => 'required|string',
            'credential' => 'required|string',
            'group' => 'nullable|string',
        ]);

        try {
            $resp = RustedClient::addDevice($data);
            if ($resp->successful()) {
                return back()->with('status', "Device {$data['name']} added.");
            }
            return back()->with('error', 'Add failed: '.($resp->json('error') ?? $resp->status()));
        } catch (\Throwable $e) {
            return back()->with('error', 'Add failed: '.$e->getMessage());
        }
    }

    public function destroy(string $name): RedirectResponse
    {
        try {
            $resp = RustedClient::removeDevice($name);
            return $resp->successful()
                ? back()->with('status', "Device {$name} removed.")
                : back()->with('error', 'Remove failed: '.($resp->json('error') ?? $resp->status()));
        } catch (\Throwable $e) {
            return back()->with('error', 'Remove failed: '.$e->getMessage());
        }
    }

    public function backup(string $name): RedirectResponse
    {
        try {
            $resp = RustedClient::backup($name);
            if (! $resp->successful()) {
                return back()->with('error', "Backup of {$name} failed: ".($resp->json('error') ?? $resp->status()));
            }
            $result = $resp->json();
            $status = $result['Status'] ?? $result['status'] ?? 'done';
            $msg = $result['Message'] ?? $result['message'] ?? '';
            return back()->with('status', "Backup of {$name}: {$status}. {$msg}");
        } catch (\Throwable $e) {
            return back()->with('error', "Backup of {$name} failed: ".$e->getMessage());
        }
    }

    public function history(string $name): View
    {
        $runs = [];
        $error = null;
        try {
            $runs = RustedClient::history($name);
        } catch (\Throwable $e) {
            $error = $e->getMessage();
        }

        return view('rusted::history', [
            'title' => "Backup history: {$name}",
            'device' => $name,
            'runs' => $runs,
            'apiError' => $error,
        ]);
    }
}
