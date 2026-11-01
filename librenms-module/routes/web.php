<?php

use Illuminate\Support\Facades\Route;
use AthenaNetworks\RustedLibrenms\Controllers\RustedController;

// Every route is admin-only (LibreNMS's `admin` gate). The plugin can change
// devices, credentials and backups, and the stored configs it serves contain
// secrets (hashed passwords, SNMP communities, keys), so nothing here is safe
// for read-only users. The controller enforces the same gate as well.
Route::middleware(['web', 'auth', 'can:admin'])->prefix('plugin/rusted')->name('rusted.')->group(function (): void {
    // The page (HTML shell).
    Route::get('/', [RustedController::class, 'index'])->name('index');

    // Bulk device-sync page (add to / disable in rusted; history preserved).
    Route::get('/sync', [RustedController::class, 'sync'])->name('sync');

    // Per-device page: latest config, revisions, diff and backup runs.
    Route::get('/device/{name}', [RustedController::class, 'device'])->name('device.show');

    // Same-origin AJAX/JSON receiver. The browser posts here; the controller
    // relays to the rusted API. No reverse proxy required.
    Route::prefix('api')->name('api.')->group(function (): void {
        Route::get('drivers', [RustedController::class, 'apiDrivers'])->name('drivers');
        Route::get('transports', [RustedController::class, 'apiTransports'])->name('transports');

        Route::get('credentials', [RustedController::class, 'apiCredentials'])->name('credentials');
        Route::post('credentials', [RustedController::class, 'apiAddCredential'])->name('credentials.add');
        Route::delete('credentials/{name}', [RustedController::class, 'apiRemoveCredential'])->name('credentials.remove');

        // Which rusted device (if any) a LibreNMS device maps to; see DeviceMap.
        Route::get('librenms/{id}', [RustedController::class, 'libreDevice'])->whereNumber('id')->name('librenms.device');

        Route::get('devices', [RustedController::class, 'apiDevices'])->name('devices');
        Route::post('devices', [RustedController::class, 'apiAddDevice'])->name('devices.add');
        Route::get('devices/{name}', [RustedController::class, 'apiDevice'])->name('devices.show');
        Route::put('devices/{name}', [RustedController::class, 'apiUpdateDevice'])->name('devices.update');
        Route::delete('devices/{name}', [RustedController::class, 'apiRemoveDevice'])->name('devices.remove');
        Route::post('devices/{name}/enabled', [RustedController::class, 'apiSetEnabled'])->name('devices.enabled');
        Route::post('devices/{name}/backup', [RustedController::class, 'apiBackup'])->name('devices.backup');
        Route::get('devices/{name}/history', [RustedController::class, 'apiHistory'])->name('devices.history');
        Route::get('devices/{name}/history/zip', [RustedController::class, 'apiHistoryZip'])->name('devices.history.zip');

        // Config retrieval, revision listing and diffing. config/diff return
        // text/plain, so they are relayed via proxyText() rather than proxy().
        Route::get('devices/{name}/config', [RustedController::class, 'apiConfig'])->name('devices.config');
        Route::get('devices/{name}/versions', [RustedController::class, 'apiVersions'])->name('devices.versions');
        Route::get('devices/{name}/diff', [RustedController::class, 'apiDiff'])->name('devices.diff');

        // Bulk sync helpers (disable, never delete, so history is preserved).
        Route::prefix('sync')->name('sync.')->group(function (): void {
            Route::get('state', [RustedController::class, 'syncState'])->name('state');
            Route::post('add', [RustedController::class, 'syncAdd'])->name('add');
            Route::post('disable', [RustedController::class, 'syncDisable'])->name('disable');
        });
    });
});
