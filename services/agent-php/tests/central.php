<?php
declare(strict_types=1);
// Test harness only. Input uses stdin so credentials never enter process arguments.
require dirname(__DIR__).'/private/src/Adapter.php';
$in=json_decode(stream_get_contents(STDIN),true,32,JSON_THROW_ON_ERROR);
try {
    $client=new \Shortlink\Client($in['origin'],true);
    if($in['action']==='activate') {
        $config=\Shortlink\activate($in['private'],['hostname'=>$in['hostname'],'controlPlaneUrl'=>$in['origin'],'developmentLoopback'=>true,'trustedProxies'=>[]],$in['token']);
        echo json_encode(['installed'=>is_file($in['private'].'/config.json'),'domainId'=>$config['domainId'],'version'=>$config['version']]);
    } else {
        $config=\Shortlink\readJson($in['private'].'/config.json');
        if($in['action']==='decision') echo json_encode(\Shortlink\decision($client,$config,'hello',['REMOTE_ADDR'=>'1.1.1.1','HTTP_USER_AGENT'=>$in['ua']]));
        elseif($in['action']==='probe') { $r=\Shortlink\probe($client,$config,\Shortlink\token()); echo json_encode(['ready'=>$r['body']['ready'],'domainId'=>$r['body']['domainId']]); }
    }
} catch(\Throwable $e) { echo json_encode(['failed'=>true]); exit(1); }
