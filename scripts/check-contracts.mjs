import SwaggerParser from '@apidevtools/swagger-parser';
const api = await SwaggerParser.validate('packages/contracts/openapi.json');
const operationIds = new Set();
let count=0;
for(const [path,item] of Object.entries(api.paths)){
  for(const method of ['get','post','patch','delete']){
    const operation=item[method];if(!operation)continue;
    if(!operation.operationId || operationIds.has(operation.operationId))throw Error(`Invalid or duplicate operationId at ${method} ${path}`);
    operationIds.add(operation.operationId);count++;
    if(!('security' in operation))throw Error(`Explicit authorization missing: ${method} ${path}`);
    if(!operation.responses.default)throw Error(`Error contract missing: ${method} ${path}`);
  }
}
console.log(`OpenAPI valid; ${count} operations have explicit auth, error responses and unique IDs.`);
