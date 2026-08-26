<?php

use Illuminate\Support\Facades\Route;
use AthenaNetworks\RustedLibrenms\Controllers\RustedController;

Route::middleware(['web', 'auth'])->prefix('plugin/rusted')->name('rusted.')->group(function (): void {
    // The page (HTML shell).
    Route::get('/', [RustedController::class, 'index'])->name('index');
    // Bulk device-sync page (add to / disable in rusted, history-preserved).
    Route::get('/sync', [RustedController::class, 'sync'])->name('sync');

    // A per-device detail page: view the latest config, browse revisions, and
    // diff two revisions.
    Route::get('/device/{name}', [RustedController::class, 'device'])->name('device.show');

    // Same-origin AJAX/JSON receiver. The browser posts here; the controller
    // relays to the rusted API. No reverse proxy required.
    Route::prefix('api')->name('api.')->group(function (): void {
        Route::get('drivers', [RustedController::class, 'apiDrivers'])->name('drivers');
        Route::get('transports', [RustedController::class, 'apiTransports'])->name('transports');

        Route::get('credentials', [RustedController::class, 'apiCredentials'])->name('credentials');
        Route::post('credentials', [RustedController::class, 'apiAddCredential'])->name('credentials.add');
        Route::delete('credentials/{name}', [RustedController::class, 'apiRemoveCredential'])->name('credentials.remove');

        Route::get('devices', [RustedController::class, 'apiDevices'])->name('devices');
        Route::post('devices', [RustedController::class, 'apiAddDevice'])->name('devices.add');
        Route::get('devices/{name}', [RustedController::class, 'apiDevice'])->name('devices.show');
        Route::put('devices/{name}', [RustedController::class, 'apiUpdateDevice'])->name('devices.update');
        Route::delete('devices/{name}', [RustedController::class, 'apiRemoveDevice'])->name('devices.remove');
        Route::post('devices/{name}/enabled', [RustedController::class, 'apiSetEnabled'])->name('devices.enabled');
        Route::post('devices/{name}/backup', [RustedController::class, 'apiBackup'])->name('devices.backup');
        Route::get('devices/{name}/history', [RustedController::class, 'apiHistory'])->name('devices.history');
        Route::get('devices/{name}/history/zip', [RustedController::class, 'apiHistoryZip'])->name('devices.history.zip');

        // Bulk export: EVERY device's full configuration history in one zip.
        Route::get('export/zip', [RustedController::class, 'apiExportZip'])->name('export.zip');

        // Configuration retrieval, revision listing and diffing. These return
        // text/plain bodies, so they are relayed via proxyText() rather than
        // proxy(), to avoid corrupting the raw config/diff payload.
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
