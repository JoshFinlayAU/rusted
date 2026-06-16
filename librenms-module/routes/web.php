<?php

use Illuminate\Support\Facades\Route;
use AthenaNetworks\RustedLibrenms\Controllers\RustedController;

Route::middleware(['web', 'auth'])->prefix('plugin/rusted')->name('rusted.')->group(function (): void {
    // The page (HTML shell).
    Route::get('/', [RustedController::class, 'index'])->name('index');

    // Same-origin AJAX/JSON receiver. The browser posts here; the controller
    // relays to the rusted API. No reverse proxy required.
    Route::prefix('api')->name('api.')->group(function (): void {
        Route::get('drivers', [RustedController::class, 'apiDrivers'])->name('drivers');

        Route::get('credentials', [RustedController::class, 'apiCredentials'])->name('credentials');
        Route::post('credentials', [RustedController::class, 'apiAddCredential'])->name('credentials.add');
        Route::delete('credentials/{name}', [RustedController::class, 'apiRemoveCredential'])->name('credentials.remove');

        Route::get('devices', [RustedController::class, 'apiDevices'])->name('devices');
        Route::post('devices', [RustedController::class, 'apiAddDevice'])->name('devices.add');
        Route::get('devices/{name}', [RustedController::class, 'apiDevice'])->name('devices.show');
        Route::delete('devices/{name}', [RustedController::class, 'apiRemoveDevice'])->name('devices.remove');
        Route::post('devices/{name}/backup', [RustedController::class, 'apiBackup'])->name('devices.backup');
        Route::get('devices/{name}/history', [RustedController::class, 'apiHistory'])->name('devices.history');
    });
});
