#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <pthread.h>
typedef struct {void *ptr; size_t len;} buffer;
typedef struct {uint32_t version;void *ctx;int (*call)(void*,const char*,const uint8_t*,size_t,buffer*);void (*free)(void*,size_t);} hostapi;
typedef struct {uint32_t version;int (*call)(const char*,const uint8_t*,size_t,buffer*);void (*free)(void*,size_t);void (*shutdown)(void);} pluginapi;
static hostapi host;
static pthread_mutex_t lock=PTHREAD_MUTEX_INITIALIZER;
static int call(const char *m,const uint8_t *r,size_t n,buffer *o){
 char *q=calloc(n+1,1);if(n)memcpy(q,r,n);
 const char *path=getenv("CPA_AUDIT_LOG");
 pthread_mutex_lock(&lock);if(path){FILE *f=fopen(path,"a");if(f){fprintf(f,"%s\t%s\n",m,q);fclose(f);}}pthread_mutex_unlock(&lock);
if(!strcmp(m,"request.intercept_before")&&strstr(q,"audit-nested")){
 char id[160]={0};char *k=strstr(q,"\"host_callback_id\"");if(k){k=strchr(k,':');if(k){k=strchr(k,'"');if(k){k++;char *end=strchr(k,'"');if(end && (end-k)<159)memcpy(id,k,end-k);}}}
 char nested[2048];snprintf(nested,sizeof(nested),"{\"host_callback_id\":\"%s\",\"entry_protocol\":\"openai\",\"exit_protocol\":\"openai\",\"model\":\"audit-model\",\"body\":\"eyJtb2RlbCI6ICJhdWRpdC1tb2RlbCIsICJtZXNzYWdlcyI6IFt7InJvbGUiOiAidXNlciIsICJjb250ZW50IjogIm5lc3RlZCBleGFtcGxlIn1dfQ==\"}",id);
 buffer reply={0};int rc=host.call(host.ctx,"host.model.execute",(const uint8_t*)nested,strlen(nested),&reply);
 pthread_mutex_lock(&lock);if(path){FILE *f=fopen(path,"a");if(f){fprintf(f,"audit.nested.result\t%.*s\n",(int)reply.len,(char*)reply.ptr);fclose(f);}}pthread_mutex_unlock(&lock);
 if(reply.ptr)host.free(reply.ptr,reply.len);(void)rc;
 }
 const char *s="{\"ok\":true,\"result\":{}}";
if(!strcmp(m,"plugin.register")||!strcmp(m,"plugin.reconfigure"))s="{\"ok\":true,\"result\":{\"schema_version\":10,\"metadata\":{\"Name\":\"audit\",\"Version\":\"1.0\",\"Author\":\"Example user\",\"GitHubRepository\":\"https://github.com/router-for-me/CLIProxyAPI\"},\"capabilities\":{\"frontend_auth_provider\":true,\"frontend_auth_provider_exclusive\":true,\"request_interceptor\":true,\"response_interceptor\":true,\"response_stream_interceptor\":true,\"request_lifecycle_plugin\":true,\"usage_plugin\":true,\"management_api\":true}}}";
else if(!strcmp(m,"frontend_auth.identifier"))s="{\"ok\":true,\"result\":{\"Identifier\":\"audit-auth\"}}";
else if(!strcmp(m,"frontend_auth.authenticate"))s=strstr(q,"audit-deny")?"{\"ok\":true,\"result\":{\"Authenticated\":false,\"DenyStatusCode\":403,\"DenyMessage\":\"audit denial\"}}":"{\"ok\":true,\"result\":{\"Authenticated\":true,\"Principal\":\"Example user\"}}";
else if(!strcmp(m,"request.intercept_before")){if(strstr(q,"audit-error"))s="{\"ok\":false,\"error\":{\"code\":\"policy_failed\",\"message\":\"audit policy error\"}}";else if(strstr(q,"audit-stop"))s="{\"ok\":true,\"result\":{\"Terminate\":true,\"StatusCode\":403,\"ResponseBody\":\"YmxvY2tlZA==\"}}";}
else if(!strcmp(m,"response.intercept_after")&&strstr(q,"audit-rewrite"))s="{\"ok\":true,\"result\":{\"Body\":\"eyJpZCI6ICJhdWRpdCIsICJvYmplY3QiOiAiY2hhdC5jb21wbGV0aW9uIiwgIm1vZGVsIjogImF1ZGl0LW1vZGVsIiwgImNob2ljZXMiOiBbeyJpbmRleCI6IDAsICJtZXNzYWdlIjogeyJyb2xlIjogImFzc2lzdGFudCIsICJjb250ZW50IjogInJld3JpdHRlbiJ9LCAiZmluaXNoX3JlYXNvbiI6ICJzdG9wIn1dLCAidXNhZ2UiOiB7InByb21wdF90b2tlbnMiOiA5OTksICJjb21wbGV0aW9uX3Rva2VucyI6IDk5OSwgInRvdGFsX3Rva2VucyI6IDE5OTh9fQ==\"}}";
else if(!strcmp(m,"response.intercept_stream_chunk")&&strstr(q,"audit-drop"))s="{\"ok\":true,\"result\":{\"DropChunk\":true}}";
else if(!strcmp(m,"management.register"))s="{\"ok\":true,\"result\":{\"Routes\":[{\"Method\":\"POST\",\"Path\":\"/audit/start\"}],\"Resources\":[{\"Path\":\"/status\"}]}}";
else if(!strcmp(m,"management.handle"))s="{\"ok\":true,\"result\":{\"StatusCode\":200,\"Body\":\"eyJhdWRpdCI6dHJ1ZX0=\"}}";
o->len=strlen(s);o->ptr=malloc(o->len);memcpy(o->ptr,s,o->len);free(q);return 0;}
static void release(void *p,size_t n){(void)n;free(p);}
static void stop(void){}
__attribute__((visibility("default"))) int cliproxy_plugin_init(const hostapi *h,pluginapi *p){host=*h;p->version=1;p->call=call;p->free=release;p->shutdown=stop;return 0;}
