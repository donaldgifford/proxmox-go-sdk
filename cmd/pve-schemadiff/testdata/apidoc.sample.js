// Synthetic Proxmox VE apidoc.js fixture (not a real dump). Demonstrates the
// tool; replace with a real apidoc.js from a live 9.x node in CI.
const apiSchema = [
   { "path" : "/version", "info" : { "GET" : {} } },
   { "path" : "/access/users", "info" : { "GET" : {}, "POST" : {} } },
   { "path" : "/nodes", "info" : { "GET" : {} },
     "children" : [
        { "path" : "/nodes/{node}/qemu", "info" : { "GET" : {}, "POST" : {} },
          "children" : [
             { "path" : "/nodes/{node}/qemu/{vmid}/vncproxy", "info" : { "POST" : {} } }
          ]
        }
     ]
   }
];
