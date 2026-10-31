{{--
    Sticky, dismissable alerts container shared by the rusted views.

    The container is fixed to the viewport (just under LibreNMS's sticky
    navbar) so messages stay visible even when the page is scrolled. Each
    alert has a &times; close button. Success messages fade after 10s;
    warnings and errors stay until the user closes them.

    Usage: @include('rusted::partials.sticky-alerts', ['id' => 'rusted-alerts'])
    then call rustedAlert(containerElement, type, message) from page JS.
--}}
@php($id = $id ?? 'rusted-alerts')
<style>
    #{{ $id }} {
        position: fixed;
        top: 56px;
        left: 50%;
        transform: translateX(-50%);
        width: min(720px, calc(100vw - 24px));
        max-height: calc(100vh - 70px);
        overflow-y: auto;
        overflow-x: hidden;
        z-index: 1060;
        pointer-events: none;
    }
    #{{ $id }} .alert {
        pointer-events: auto;
        white-space: pre-line;
        box-shadow: 0 3px 12px rgba(0, 0, 0, 0.25);
        animation: rusted-alert-in 0.18s ease-out;
    }
    @keyframes rusted-alert-in {
        from { opacity: 0; transform: translateY(-8px); }
        to { opacity: 1; transform: translateY(0); }
    }
</style>
<div id="{{ $id }}"></div>
<script>
    // Shared by every rusted view. The message is inserted as text, never HTML.
    window.rustedAlert = window.rustedAlert || function (container, type, msg) {
        const div = document.createElement('div');
        div.className = 'alert alert-' + type + ' alert-dismissable';
        const close = document.createElement('button');
        close.type = 'button';
        close.className = 'close';
        close.innerHTML = '&times;';
        close.addEventListener('click', function () { div.remove(); });
        div.appendChild(close);
        div.appendChild(document.createTextNode(String(msg)));
        container.appendChild(div);
        if (type === 'success') {
            setTimeout(function () { div.remove(); }, 10000);
        }
    };
</script>
