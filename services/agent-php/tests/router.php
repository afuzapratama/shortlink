<?php
// Development server router only; never included in the release ZIP.
$path=parse_url($_SERVER['REQUEST_URI'],PHP_URL_PATH);
if ($path==='/setup.css') return false;
require dirname(__DIR__).'/public/index.php';
