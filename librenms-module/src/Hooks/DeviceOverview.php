<?php

namespace AthenaNetworks\RustedLibrenms\Hooks;

use App\Models\Device;
use App\Models\User;
use Illuminate\Contracts\View\View;
use LibreNMS\Interfaces\Plugins\Hooks\DeviceOverviewHook;

/**
 * Renders a "Configuration Backups" panel on every device's Overview page.
 *
 * The panel itself is loaded by LibreNMS server-side; its contents are then
 * populated by the browser via the same-origin AJAX receiver, which first asks
 * which rusted device this LibreNMS device maps to (see Support\DeviceMap).
 */
class DeviceOverview implements DeviceOverviewHook
{
    /** The panel's AJAX routes are admin-only, so only admins see the panel. */
    public function authorize(User $user, Device $device): bool
    {
        return $user->can('admin');
    }

    public function handle(string $pluginName, array $settings, Device $device): View
    {
        return view("$pluginName::device-overview", [
            'deviceId' => $device->device_id,
            'deviceBase' => url('plugin/rusted/device'),
        ]);
    }
}
