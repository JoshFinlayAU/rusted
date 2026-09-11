{{--
    Sticky, dismissible alerts container shared by all rusted views.

    The container is fixed to the viewport (just under LibreNMS's sticky
    navbar, which has z-index 1100) so error/success messages stay visible
    even when the page is scrolled and the top of the content is off-screen.
    Messages never auto-dismiss: the user closes each one with the &times;
    button and any later messages stack below until they too are read.

    Usage: @include('rusted::partials.sticky-alerts', ['id' => 'rusted-alerts'])
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
