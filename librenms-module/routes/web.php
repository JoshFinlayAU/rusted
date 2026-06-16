<?php

use Illuminate\Support\Facades\Route;
use AthenaNetworks\RustedLibrenms\Controllers\RustedController;

Route::middleware(['web', 'auth'])->prefix('plugin/rusted')->name('rusted.')->group(function (): void {
    Route::get('/', [RustedController::class, 'index'])->name('index');
    Route::post('devices', [RustedController::class, 'store'])->name('devices.store');
    Route::delete('devices/{name}', [RustedController::class, 'destroy'])->name('devices.destroy');
    Route::post('devices/{name}/backup', [RustedController::class, 'backup'])->name('devices.backup');
    Route::get('devices/{name}/history', [RustedController::class, 'history'])->name('devices.history');
});
