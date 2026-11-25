var Fe=(function(){"use strict";let o=()=>{},e={morphStyle:"outerHTML",callbacks:{beforeNodeAdded:o,afterNodeAdded:o,beforeNodeMorphed:o,afterNodeMorphed:o,beforeNodeRemoved:o,afterNodeRemoved:o,beforeAttributeUpdated:o},head:{style:"merge",shouldPreserve:b=>b.getAttribute("im-preserve")==="true",shouldReAppend:b=>b.getAttribute("im-re-append")==="true",shouldRemove:o,afterHeadMorphed:o},restoreFocus:!0};function t(b,y,x={}){b=v(b);let S=f(y),C=h(b,S,x),$=r(C,()=>p(C,b,S,u=>u.morphStyle==="innerHTML"?(i(u,b,S),Array.from(b.childNodes)):n(u,b,S)));return C.pantry.remove(),$}function n(b,y,x){let S=f(y);return i(b,S,x,y,y.nextSibling),Array.from(S.childNodes)}function r(b,y){if(!b.config.restoreFocus)return y();let x=document.activeElement;if(!(x instanceof HTMLInputElement||x instanceof HTMLTextAreaElement))return y();let{id:S,selectionStart:C,selectionEnd:$}=x,u=y();return S&&S!==document.activeElement?.getAttribute("id")&&(x=b.target.querySelector(`[id="${S}"]`),x?.focus()),x&&!x.selectionEnd&&$&&x.setSelectionRange(C,$),u}let i=(function(){function b(l,c,g,m=null,k=null){c instanceof HTMLTemplateElement&&g instanceof HTMLTemplateElement&&(c=c.content,g=g.content),m||=c.firstChild;for(let E of g.childNodes){if(m&&m!=k){let O=x(l,E,m,k);if(O){O!==m&&C(l,m,O),s(O,E,l),m=O.nextSibling;continue}}if(E instanceof Element){let O=E.getAttribute("id");if(l.persistentIds.has(O)){let D=$(c,O,m,l);s(D,E,l),m=D.nextSibling;continue}}let A=y(c,E,m,l);A&&(m=A.nextSibling)}for(;m&&m!=k;){let E=m;m=m.nextSibling,S(l,E)}}function y(l,c,g,m){if(m.callbacks.beforeNodeAdded(c)===!1)return null;if(m.idMap.has(c)){let k=document.createElement(c.tagName);return l.insertBefore(k,g),s(k,c,m),m.callbacks.afterNodeAdded(k),k}else{let k=document.importNode(c,!0);return l.insertBefore(k,g),m.callbacks.afterNodeAdded(k),k}}let x=(function(){function l(m,k,E,A){let O=null,D=k.nextSibling,Ve=0,M=E;for(;M&&M!=A;){if(g(M,k)){if(c(m,M,k))return M;O===null&&(m.idMap.has(M)||(O=M))}if(O===null&&D&&g(M,D)&&(Ve++,D=D.nextSibling,Ve>=2&&(O=void 0)),m.activeElementAndParents.includes(M))break;M=M.nextSibling}return O||null}function c(m,k,E){let A=m.idMap.get(k),O=m.idMap.get(E);if(!O||!A)return!1;for(let D of A)if(O.has(D))return!0;return!1}function g(m,k){let E=m,A=k;return E.nodeType===A.nodeType&&E.tagName===A.tagName&&(!E.getAttribute?.("id")||E.getAttribute?.("id")===A.getAttribute?.("id"))}return l})();function S(l,c){if(l.idMap.has(c))_(l.pantry,c,null);else{if(l.callbacks.beforeNodeRemoved(c)===!1)return;c.parentNode?.removeChild(c),l.callbacks.afterNodeRemoved(c)}}function C(l,c,g){let m=c;for(;m&&m!==g;){let k=m;m=m.nextSibling,S(l,k)}return m}function $(l,c,g,m){let k=m.target.getAttribute?.("id")===c&&m.target||m.target.querySelector(`[id="${c}"]`)||m.pantry.querySelector(`[id="${c}"]`);return u(k,m),_(l,k,g),k}function u(l,c){let g=l.getAttribute("id");for(;l=l.parentNode;){let m=c.idMap.get(l);m&&(m.delete(g),m.size||c.idMap.delete(l))}}function _(l,c,g){if(l.moveBefore)try{l.moveBefore(c,g)}catch{l.insertBefore(c,g)}else l.insertBefore(c,g)}return b})(),s=(function(){function b(u,_,l){return l.ignoreActive&&u===document.activeElement?null:(l.callbacks.beforeNodeMorphed(u,_)===!1||(u instanceof HTMLHeadElement&&l.head.ignore||(u instanceof HTMLHeadElement&&l.head.style!=="morph"?a(u,_,l):(y(u,_,l),$(u,l)||i(l,u,_))),l.callbacks.afterNodeMorphed(u,_)),u)}function y(u,_,l){let c=_.nodeType;if(c===1){let g=u,m=_,k=g.attributes,E=m.attributes;for(let A of E)C(A.name,g,"update",l)||g.getAttribute(A.name)!==A.value&&g.setAttribute(A.name,A.value);for(let A=k.length-1;0<=A;A--){let O=k[A];if(O&&!m.hasAttribute(O.name)){if(C(O.name,g,"remove",l))continue;g.removeAttribute(O.name)}}$(g,l)||x(g,m,l)}(c===8||c===3)&&u.nodeValue!==_.nodeValue&&(u.nodeValue=_.nodeValue)}function x(u,_,l){if(u instanceof HTMLInputElement&&_ instanceof HTMLInputElement&&_.type!=="file"){let c=_.value,g=u.value;S(u,_,"checked",l),S(u,_,"disabled",l),_.hasAttribute("value")?g!==c&&(C("value",u,"update",l)||(u.setAttribute("value",c),u.value=c)):C("value",u,"remove",l)||(u.value="",u.removeAttribute("value"))}else if(u instanceof HTMLOptionElement&&_ instanceof HTMLOptionElement)S(u,_,"selected",l);else if(u instanceof HTMLTextAreaElement&&_ instanceof HTMLTextAreaElement){let c=_.value,g=u.value;if(C("value",u,"update",l))return;c!==g&&(u.value=c),u.firstChild&&u.firstChild.nodeValue!==c&&(u.firstChild.nodeValue=c)}}function S(u,_,l,c){let g=_[l],m=u[l];if(g!==m){let k=C(l,u,"update",c);k||(u[l]=_[l]),g?k||u.setAttribute(l,""):C(l,u,"remove",c)||u.removeAttribute(l)}}function C(u,_,l,c){return u==="value"&&c.ignoreActiveValue&&_===document.activeElement?!0:c.callbacks.beforeAttributeUpdated(u,_,l)===!1}function $(u,_){return!!_.ignoreActiveValue&&u===document.activeElement&&u!==document.body}return b})();function p(b,y,x,S){if(b.head.block){let C=y.querySelector("head"),$=x.querySelector("head");if(C&&$){let u=a(C,$,b);return Promise.all(u).then(()=>{let _=Object.assign(b,{head:{block:!1,ignore:!0}});return S(_)})}}return S(b)}function a(b,y,x){let S=[],C=[],$=[],u=[],_=new Map;for(let c of y.children)_.set(c.outerHTML,c);for(let c of b.children){let g=_.has(c.outerHTML),m=x.head.shouldReAppend(c),k=x.head.shouldPreserve(c);g||k?m?C.push(c):(_.delete(c.outerHTML),$.push(c)):x.head.style==="append"?m&&(C.push(c),u.push(c)):x.head.shouldRemove(c)!==!1&&C.push(c)}u.push(..._.values());let l=[];for(let c of u){let g=document.createRange().createContextualFragment(c.outerHTML).firstChild;if(x.callbacks.beforeNodeAdded(g)!==!1){if("href"in g&&g.href||"src"in g&&g.src){let m,k=new Promise(function(E){m=E});g.addEventListener("load",function(){m()}),l.push(k)}b.appendChild(g),x.callbacks.afterNodeAdded(g),S.push(g)}}for(let c of C)x.callbacks.beforeNodeRemoved(c)!==!1&&(b.removeChild(c),x.callbacks.afterNodeRemoved(c));return x.head.afterHeadMorphed(b,{added:S,kept:$,removed:C}),l}let h=(function(){function b(l,c,g){let{persistentIds:m,idMap:k}=u(l,c),E=y(g),A=E.morphStyle||"outerHTML";if(!["innerHTML","outerHTML"].includes(A))throw`Do not understand how to morph style ${A}`;return{target:l,newContent:c,config:E,morphStyle:A,ignoreActive:E.ignoreActive,ignoreActiveValue:E.ignoreActiveValue,restoreFocus:E.restoreFocus,idMap:k,persistentIds:m,pantry:x(),activeElementAndParents:S(l),callbacks:E.callbacks,head:E.head}}function y(l){let c=Object.assign({},e);return Object.assign(c,l),c.callbacks=Object.assign({},e.callbacks,l.callbacks),c.head=Object.assign({},e.head,l.head),c}function x(){let l=document.createElement("div");return l.hidden=!0,document.body.insertAdjacentElement("afterend",l),l}function S(l){let c=[],g=document.activeElement;if(g?.tagName!=="BODY"&&l.contains(g))for(;g&&(c.push(g),g!==l);)g=g.parentElement;return c}function C(l){let c=Array.from(l.querySelectorAll("[id]"));return l.getAttribute?.("id")&&c.push(l),c}function $(l,c,g,m){for(let k of m){let E=k.getAttribute("id");if(c.has(E)){let A=k;for(;A;){let O=l.get(A);if(O==null&&(O=new Set,l.set(A,O)),O.add(E),A===g)break;A=A.parentElement}}}}function u(l,c){let g=C(l),m=C(c),k=_(g,m),E=new Map;$(E,k,l,g);let A=c.__idiomorphRoot||c;return $(E,k,A,m),{persistentIds:k,idMap:E}}function _(l,c){let g=new Set,m=new Map;for(let{id:E,tagName:A}of l)m.has(E)?g.add(E):m.set(E,A);let k=new Set;for(let{id:E,tagName:A}of c)k.has(E)?g.add(E):m.get(E)===A&&k.add(E);for(let E of g)k.delete(E);return k}return b})(),{normalizeElement:v,normalizeParent:f}=(function(){let b=new WeakSet;function y($){return $ instanceof Document?$.documentElement:$}function x($){if($==null)return document.createElement("div");if(typeof $=="string")return x(C($));if(b.has($))return $;if($ instanceof Node){if($.parentNode)return new S($);{let u=document.createElement("div");return u.append($),u}}else{let u=document.createElement("div");for(let _ of[...$])u.append(_);return u}}class S{constructor(u){this.originalNode=u,this.realParentNode=u.parentNode,this.previousSibling=u.previousSibling,this.nextSibling=u.nextSibling}get childNodes(){let u=[],_=this.previousSibling?this.previousSibling.nextSibling:this.realParentNode.firstChild;for(;_&&_!=this.nextSibling;)u.push(_),_=_.nextSibling;return u}querySelectorAll(u){return this.childNodes.reduce((_,l)=>{if(l instanceof Element){l.matches(u)&&_.push(l);let c=l.querySelectorAll(u);for(let g=0;g<c.length;g++)_.push(c[g])}return _},[])}insertBefore(u,_){return this.realParentNode.insertBefore(u,_)}moveBefore(u,_){return this.realParentNode.moveBefore(u,_)}get __idiomorphRoot(){return this.originalNode}}function C($){let u=new DOMParser,_=$.replace(/<svg(\s[^>]*>|>)([\s\S]*?)<\/svg>/gim,"");if(_.match(/<\/html>/)||_.match(/<\/head>/)||_.match(/<\/body>/)){let l=u.parseFromString($,"text/html");if(_.match(/<\/html>/))return b.add(l),l;{let c=l.firstChild;return c&&b.add(c),c}}else{let c=u.parseFromString("<body><template>"+$+"</template></body>","text/html").body.querySelector("template").content;return b.add(c),c}}return{normalizeElement:y,normalizeParent:x}})();return{morph:t,defaults:e}})();function W(o){return o?o.charAt(0)==="/"?o:"/"+o:"/"}function T(){var o=document.querySelector("base");if(!o||!o.href)return"";var e=new URL(o.href,window.location.href).pathname;return e==="/"?"":e.replace(/\/+$/,"")}function ie(){var o=T(),e=window.location.pathname||"/";return o&&(e===o||e.indexOf(o+"/")===0)&&(e=e.slice(o.length)),W(e)}function We(){return customElements.get("bn-context")?Promise.resolve():customElements.whenDefined("bn-context")}function Qe(o,e){if(!o)return console.debug("bino: swapContext skipped - empty html"),!1;e||(e=new DOMParser);var t=e.parseFromString(o,"text/html"),n=t.querySelector("bn-context"),r=document.querySelector("bn-context");return n?r?(St(r,n),Fe.morph(r,n.innerHTML,{morphStyle:"innerHTML",callbacks:{beforeAttributeUpdated:function(i,s,p){if(i==="class"&&s.tagName&&s.tagName.includes("-"))return!1}}}),!0):(console.debug("bino: swapContext skipped - live DOM has no <bn-context>"),!1):(console.debug("bino: swapContext skipped - incoming HTML has no <bn-context>"),!1)}function St(o,e){for(var t=0;t<e.attributes.length;t++){var n=e.attributes[t];n.name!=="class"&&o.getAttribute(n.name)!==n.value&&o.setAttribute(n.name,n.value)}for(var r=o.attributes.length-1;r>=0;r--){var i=o.attributes[r].name;i!=="class"&&(e.hasAttribute(i)||o.removeAttribute(i))}}var Je=[["rowGroup","data-bn-row-group"],["category","data-bn-category"],["subCategory","data-bn-sub-category"],["date","data-bn-date"],["scenario","data-bn-scenario"],["row","data-bn-row"]],At=["Text","Tree","Grid","SmallMultiple"],Ct=["bn-chart-time","bn-chart-structure","bn-chart-scatter","bn-chart-bubble","bn-chart-bullet","bn-table"],Ye=o=>!!o&&o.nodeType===1;function Ot(o){let e=o.getAttribute("data-bn-part");if(!e)return null;let t={};for(let[n,r]of Je){let i=o.getAttribute(r);i!==null&&i!==""&&(t[n]=i)}return e!=="element"&&(t.part=e),t}function zt(o){for(let e of o){let t=Ot(e);if(t)return{point:t,stopped:!1};if(e.localName==="td"||e.localName==="th")return{point:null,stopped:!0}}return{point:null,stopped:!1}}function Ge(o){let e=o.filter(Ye),t=e.findIndex(f=>f.hasAttribute("data-bino-target-kind")||f.hasAttribute("data-bino-kind")&&(f.getAttribute("data-bino-ref")||f.getAttribute("data-bino-name")));if(t<0)return null;let n=e[t],r=n.hasAttribute("data-bino-target-kind"),i=r?n.getAttribute("data-bino-target-kind"):n.getAttribute("data-bino-kind"),s=r?n.getAttribute("data-bino-target-name"):n.getAttribute("data-bino-ref")||n.getAttribute("data-bino-name"),p=e.slice(0,t),a={kind:i,name:s,slot:n,inner:p,host:[...p].reverse().find(f=>Ct.includes(f.localName))};if(i==="SmallMultiple"){let f=p.find(b=>b.hasAttribute("data-bino-panel"));f&&(a.panel=f.getAttribute("data-bino-panel"))}let h=e.slice(t).find(f=>f.hasAttribute("data-bino-source-line")),v=h?parseInt(h.getAttribute("data-bino-source-line")||"",10):0;return v>0&&(a.sourceFile=h.getAttribute("data-bino-source-file")||"",a.line=v),a}function Lt(o,e,t,n){let r=e.getBoundingClientRect(),i=e.offsetWidth?r.width/e.offsetWidth:1,s=(t-r.left)/i,p=(n-r.top)/i,a=null,h=1/0;for(let f of o&&o.elements||[]){let b=f.rect&&f.rect.component;if(!f.point||!b||s<b.x||s>b.x+b.width||p<b.y||p>b.y+b.height)continue;let y=b.width*b.height;y<h&&(h=y,a=f.point)}if(!a)return null;let v={};for(let[f]of Je)a[f]!==void 0&&a[f]!==null&&a[f]!==""&&(v[f]=String(a[f]));return a.part&&a.part!=="element"&&(v.part=a.part),v}function Tt(o,e){if(!o||typeof o.getLayoutState!="function")return Promise.resolve(null);let t;return Promise.race([o.getLayoutState({detail:"full"}),new Promise(n=>{t=setTimeout(()=>n(null),e)})]).catch(()=>null).finally(()=>clearTimeout(t))}async function Mt(o,e,t){let n=Ge(o);if(!n)return null;let r={kind:n.kind,name:n.name};n.panel&&(r.panel=n.panel),n.sourceFile!==void 0&&(r.sourceFile=n.sourceFile,r.line=n.line);let i=n.kind==="SmallMultiple"&&n.panel?"panel":n.kind;if(At.includes(i))return r;let s=zt(n.inner);if(s.stopped)return r;let p=s.point;if(!p&&n.host){let a=await Tt(n.host,1e3);p=Lt(a,n.host,e,t)}return p&&Object.keys(p).length&&(r.point=p),r}function Xe(o){let e=o||window;if(e.__binoAnnotate||e.parent===e)return;e.__binoAnnotate=!0;let t=e.document,n=!1,r=0,i=null,s=()=>(i||(i=t.createElement("div"),i.setAttribute("data-bino-annotate-outline",""),i.style.cssText="position:fixed;pointer-events:none;z-index:2147483647;outline:2px dashed #0070c0;outline-offset:1px;display:none",t.body.appendChild(i)),i),p=()=>{i&&(i.style.display="none")},a=h=>{let v=h.getBoundingClientRect(),f=s();f.style.left=v.left+"px",f.style.top=v.top+"px",f.style.width=v.width+"px",f.style.height=v.height+"px",f.style.display="block"};e.addEventListener("message",h=>{!h.data||h.data.type!=="bino:annotateMode"||(n=!!h.data.on,t.documentElement.style.cursor=n?"crosshair":"",n||p())}),t.addEventListener("mousemove",h=>{if(!n)return;let v=h.composedPath(),f=Ge(v);if(!f){p();return}let b=v.filter(Ye),y=f.inner.find(x=>x.hasAttribute("data-bn-part"));a(y||b[b.indexOf(f.slot)])},!0),t.addEventListener("click",h=>{if(!n||h.metaKey||h.ctrlKey||h.button!==0)return;h.preventDefault(),h.stopPropagation();let v=++r;Mt(h.composedPath(),h.clientX,h.clientY).then(f=>{v!==r||!f||e.parent.postMessage(Object.assign({type:"bino:pickPoint"},f),"*")})},!0),e.parent.postMessage({type:"bino:capabilities",annotate:1},"*")}var oe=globalThis,se=oe.ShadowRoot&&(oe.ShadyCSS===void 0||oe.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,fe=Symbol(),Ze=new WeakMap,Q=class{constructor(e,t,n){if(this._$cssResult$=!0,n!==fe)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(se&&e===void 0){let n=t!==void 0&&t.length===1;n&&(e=Ze.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),n&&Ze.set(t,e))}return e}toString(){return this.cssText}},et=o=>new Q(typeof o=="string"?o:o+"",void 0,fe),L=(o,...e)=>{let t=o.length===1?o[0]:e.reduce((n,r,i)=>n+(s=>{if(s._$cssResult$===!0)return s.cssText;if(typeof s=="number")return s;throw Error("Value passed to 'css' function must be a 'css' function result: "+s+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+o[i+1],o[0]);return new Q(t,o,fe)},tt=(o,e)=>{if(se)o.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let n=document.createElement("style"),r=oe.litNonce;r!==void 0&&n.setAttribute("nonce",r),n.textContent=t.cssText,o.appendChild(n)}},ve=se?o=>o:o=>o instanceof CSSStyleSheet?(e=>{let t="";for(let n of e.cssRules)t+=n.cssText;return et(t)})(o):o;var{is:Dt,defineProperty:Rt,getOwnPropertyDescriptor:Pt,getOwnPropertyNames:Nt,getOwnPropertySymbols:It,getPrototypeOf:Ht}=Object,ae=globalThis,nt=ae.trustedTypes,qt=nt?nt.emptyScript:"",Ut=ae.reactiveElementPolyfillSupport,J=(o,e)=>o,me={toAttribute(o,e){switch(e){case Boolean:o=o?qt:null;break;case Object:case Array:o=o==null?o:JSON.stringify(o)}return o},fromAttribute(o,e){let t=o;switch(e){case Boolean:t=o!==null;break;case Number:t=o===null?null:Number(o);break;case Object:case Array:try{t=JSON.parse(o)}catch{t=null}}return t}},it=(o,e)=>!Dt(o,e),rt={attribute:!0,type:String,converter:me,reflect:!1,useDefault:!1,hasChanged:it};Symbol.metadata??=Symbol("metadata"),ae.litPropertyMetadata??=new WeakMap;var R=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??=[]).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=rt){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let n=Symbol(),r=this.getPropertyDescriptor(e,n,t);r!==void 0&&Rt(this.prototype,e,r)}}static getPropertyDescriptor(e,t,n){let{get:r,set:i}=Pt(this.prototype,e)??{get(){return this[t]},set(s){this[t]=s}};return{get:r,set(s){let p=r?.call(this);i?.call(this,s),this.requestUpdate(e,p,n)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??rt}static _$Ei(){if(this.hasOwnProperty(J("elementProperties")))return;let e=Ht(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(J("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(J("properties"))){let t=this.properties,n=[...Nt(t),...It(t)];for(let r of n)this.createProperty(r,t[r])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[n,r]of t)this.elementProperties.set(n,r)}this._$Eh=new Map;for(let[t,n]of this.elementProperties){let r=this._$Eu(t,n);r!==void 0&&this._$Eh.set(r,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let n=new Set(e.flat(1/0).reverse());for(let r of n)t.unshift(ve(r))}else e!==void 0&&t.push(ve(e));return t}static _$Eu(e,t){let n=t.attribute;return n===!1?void 0:typeof n=="string"?n:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??=new Set).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let n of t.keys())this.hasOwnProperty(n)&&(e.set(n,this[n]),delete this[n]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return tt(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,n){this._$AK(e,n)}_$ET(e,t){let n=this.constructor.elementProperties.get(e),r=this.constructor._$Eu(e,n);if(r!==void 0&&n.reflect===!0){let i=(n.converter?.toAttribute!==void 0?n.converter:me).toAttribute(t,n.type);this._$Em=e,i==null?this.removeAttribute(r):this.setAttribute(r,i),this._$Em=null}}_$AK(e,t){let n=this.constructor,r=n._$Eh.get(e);if(r!==void 0&&this._$Em!==r){let i=n.getPropertyOptions(r),s=typeof i.converter=="function"?{fromAttribute:i.converter}:i.converter?.fromAttribute!==void 0?i.converter:me;this._$Em=r;let p=s.fromAttribute(t,i.type);this[r]=p??this._$Ej?.get(r)??p,this._$Em=null}}requestUpdate(e,t,n,r=!1,i){if(e!==void 0){let s=this.constructor;if(r===!1&&(i=this[e]),n??=s.getPropertyOptions(e),!((n.hasChanged??it)(i,t)||n.useDefault&&n.reflect&&i===this._$Ej?.get(e)&&!this.hasAttribute(s._$Eu(e,n))))return;this.C(e,t,n)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:n,reflect:r,wrapped:i},s){n&&!(this._$Ej??=new Map).has(e)&&(this._$Ej.set(e,s??t??this[e]),i!==!0||s!==void 0)||(this._$AL.has(e)||(this.hasUpdated||n||(t=void 0),this._$AL.set(e,t)),r===!0&&this._$Em!==e&&(this._$Eq??=new Set).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[r,i]of this._$Ep)this[r]=i;this._$Ep=void 0}let n=this.constructor.elementProperties;if(n.size>0)for(let[r,i]of n){let{wrapped:s}=i,p=this[r];s!==!0||this._$AL.has(r)||p===void 0||this.C(r,void 0,i,p)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(n=>n.hostUpdate?.()),this.update(t)):this._$EM()}catch(n){throw e=!1,this._$EM(),n}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&=this._$Eq.forEach(t=>this._$ET(t,this[t])),this._$EM()}updated(e){}firstUpdated(e){}};R.elementStyles=[],R.shadowRootOptions={mode:"open"},R[J("elementProperties")]=new Map,R[J("finalized")]=new Map,Ut?.({ReactiveElement:R}),(ae.reactiveElementVersions??=[]).push("2.1.2");var ke=globalThis,ot=o=>o,le=ke.trustedTypes,st=le?le.createPolicy("lit-html",{createHTML:o=>o}):void 0,ht="$lit$",N=`lit$${Math.random().toFixed(9).slice(2)}$`,pt="?"+N,Bt=`<${pt}>`,U=document,G=()=>U.createComment(""),X=o=>o===null||typeof o!="object"&&typeof o!="function",Ee=Array.isArray,Kt=o=>Ee(o)||typeof o?.[Symbol.iterator]=="function",ge=`[ 	
\f\r]`,Y=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,at=/-->/g,lt=/>/g,H=RegExp(`>|${ge}(?:([^\\s"'>=/]+)(${ge}*=${ge}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),dt=/'/g,ct=/"/g,bt=/^(?:script|style|textarea|title)$/i,Se=o=>(e,...t)=>({_$litType$:o,strings:e,values:t}),d=Se(1),Ae=Se(2),$n=Se(3),B=Symbol.for("lit-noChange"),w=Symbol.for("lit-nothing"),ut=new WeakMap,q=U.createTreeWalker(U,129);function ft(o,e){if(!Ee(o)||!o.hasOwnProperty("raw"))throw Error("invalid template strings array");return st!==void 0?st.createHTML(e):e}var jt=(o,e)=>{let t=o.length-1,n=[],r,i=e===2?"<svg>":e===3?"<math>":"",s=Y;for(let p=0;p<t;p++){let a=o[p],h,v,f=-1,b=0;for(;b<a.length&&(s.lastIndex=b,v=s.exec(a),v!==null);)b=s.lastIndex,s===Y?v[1]==="!--"?s=at:v[1]!==void 0?s=lt:v[2]!==void 0?(bt.test(v[2])&&(r=RegExp("</"+v[2],"g")),s=H):v[3]!==void 0&&(s=H):s===H?v[0]===">"?(s=r??Y,f=-1):v[1]===void 0?f=-2:(f=s.lastIndex-v[2].length,h=v[1],s=v[3]===void 0?H:v[3]==='"'?ct:dt):s===ct||s===dt?s=H:s===at||s===lt?s=Y:(s=H,r=void 0);let y=s===H&&o[p+1].startsWith("/>")?" ":"";i+=s===Y?a+Bt:f>=0?(n.push(h),a.slice(0,f)+ht+a.slice(f)+N+y):a+N+(f===-2?p:y)}return[ft(o,i+(o[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),n]},Z=class o{constructor({strings:e,_$litType$:t},n){let r;this.parts=[];let i=0,s=0,p=e.length-1,a=this.parts,[h,v]=jt(e,t);if(this.el=o.createElement(h,n),q.currentNode=this.el.content,t===2||t===3){let f=this.el.content.firstChild;f.replaceWith(...f.childNodes)}for(;(r=q.nextNode())!==null&&a.length<p;){if(r.nodeType===1){if(r.hasAttributes())for(let f of r.getAttributeNames())if(f.endsWith(ht)){let b=v[s++],y=r.getAttribute(f).split(N),x=/([.?@])?(.*)/.exec(b);a.push({type:1,index:i,name:x[2],strings:y,ctor:x[1]==="."?ye:x[1]==="?"?xe:x[1]==="@"?we:F}),r.removeAttribute(f)}else f.startsWith(N)&&(a.push({type:6,index:i}),r.removeAttribute(f));if(bt.test(r.tagName)){let f=r.textContent.split(N),b=f.length-1;if(b>0){r.textContent=le?le.emptyScript:"";for(let y=0;y<b;y++)r.append(f[y],G()),q.nextNode(),a.push({type:2,index:++i});r.append(f[b],G())}}}else if(r.nodeType===8)if(r.data===pt)a.push({type:2,index:i});else{let f=-1;for(;(f=r.data.indexOf(N,f+1))!==-1;)a.push({type:7,index:i}),f+=N.length-1}i++}}static createElement(e,t){let n=U.createElement("template");return n.innerHTML=e,n}};function V(o,e,t=o,n){if(e===B)return e;let r=n!==void 0?t._$Co?.[n]:t._$Cl,i=X(e)?void 0:e._$litDirective$;return r?.constructor!==i&&(r?._$AO?.(!1),i===void 0?r=void 0:(r=new i(o),r._$AT(o,t,n)),n!==void 0?(t._$Co??=[])[n]=r:t._$Cl=r),r!==void 0&&(e=V(o,r._$AS(o,e.values),r,n)),e}var _e=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:n}=this._$AD,r=(e?.creationScope??U).importNode(t,!0);q.currentNode=r;let i=q.nextNode(),s=0,p=0,a=n[0];for(;a!==void 0;){if(s===a.index){let h;a.type===2?h=new ee(i,i.nextSibling,this,e):a.type===1?h=new a.ctor(i,a.name,a.strings,this,e):a.type===6&&(h=new $e(i,this,e)),this._$AV.push(h),a=n[++p]}s!==a?.index&&(i=q.nextNode(),s++)}return q.currentNode=U,r}p(e){let t=0;for(let n of this._$AV)n!==void 0&&(n.strings!==void 0?(n._$AI(e,n,t),t+=n.strings.length-2):n._$AI(e[t])),t++}},ee=class o{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,n,r){this.type=2,this._$AH=w,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=n,this.options=r,this._$Cv=r?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=V(this,e,t),X(e)?e===w||e==null||e===""?(this._$AH!==w&&this._$AR(),this._$AH=w):e!==this._$AH&&e!==B&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):Kt(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==w&&X(this._$AH)?this._$AA.nextSibling.data=e:this.T(U.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:n}=e,r=typeof n=="number"?this._$AC(e):(n.el===void 0&&(n.el=Z.createElement(ft(n.h,n.h[0]),this.options)),n);if(this._$AH?._$AD===r)this._$AH.p(t);else{let i=new _e(r,this),s=i.u(this.options);i.p(t),this.T(s),this._$AH=i}}_$AC(e){let t=ut.get(e.strings);return t===void 0&&ut.set(e.strings,t=new Z(e)),t}k(e){Ee(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,n,r=0;for(let i of e)r===t.length?t.push(n=new o(this.O(G()),this.O(G()),this,this.options)):n=t[r],n._$AI(i),r++;r<t.length&&(this._$AR(n&&n._$AB.nextSibling,r),t.length=r)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let n=ot(e).nextSibling;ot(e).remove(),e=n}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},F=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,n,r,i){this.type=1,this._$AH=w,this._$AN=void 0,this.element=e,this.name=t,this._$AM=r,this.options=i,n.length>2||n[0]!==""||n[1]!==""?(this._$AH=Array(n.length-1).fill(new String),this.strings=n):this._$AH=w}_$AI(e,t=this,n,r){let i=this.strings,s=!1;if(i===void 0)e=V(this,e,t,0),s=!X(e)||e!==this._$AH&&e!==B,s&&(this._$AH=e);else{let p=e,a,h;for(e=i[0],a=0;a<i.length-1;a++)h=V(this,p[n+a],t,a),h===B&&(h=this._$AH[a]),s||=!X(h)||h!==this._$AH[a],h===w?e=w:e!==w&&(e+=(h??"")+i[a+1]),this._$AH[a]=h}s&&!r&&this.j(e)}j(e){e===w?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},ye=class extends F{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===w?void 0:e}},xe=class extends F{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==w)}},we=class extends F{constructor(e,t,n,r,i){super(e,t,n,r,i),this.type=5}_$AI(e,t=this){if((e=V(this,e,t,0)??w)===B)return;let n=this._$AH,r=e===w&&n!==w||e.capture!==n.capture||e.once!==n.once||e.passive!==n.passive,i=e!==w&&(n===w||r);r&&this.element.removeEventListener(this.name,this,n),i&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},$e=class{constructor(e,t,n){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=n}get _$AU(){return this._$AM._$AU}_$AI(e){V(this,e)}};var Vt=ke.litHtmlPolyfillSupport;Vt?.(Z,ee),(ke.litHtmlVersions??=[]).push("3.3.2");var vt=(o,e,t)=>{let n=t?.renderBefore??e,r=n._$litPart$;if(r===void 0){let i=t?.renderBefore??null;n._$litPart$=r=new ee(e.insertBefore(G(),i),i,void 0,t??{})}return r._$AI(o),r};var Ce=globalThis,z=class extends R{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let e=super.createRenderRoot();return this.renderOptions.renderBefore??=e.firstChild,e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=vt(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return B}};z._$litElement$=!0,z.finalized=!0,Ce.litElementHydrateSupport?.({LitElement:z});var Ft=Ce.litElementPolyfillSupport;Ft?.({LitElement:z});(Ce.litElementVersions??=[]).push("4.2.2");var gt="bn-annotation-layer, bn-annotation-list, bn-body, bn-card, bn-chart-bubble, bn-chart-bullet, bn-chart-scatter, bn-chart-structure, bn-chart-time, bn-footer, bn-grid, bn-image, bn-layout-card, bn-layout-page, bn-message, bn-page, bn-table, bn-text, bn-title, bn-tree";function Oe(){return document.querySelector("bn-context")}function de(){var o=Oe();return!!o&&typeof o.getLayoutState=="function"}function mt(o){return new Promise(function(e){setTimeout(e,o)})}function Wt(o){var e=[];return Array.prototype.forEach.call(o.querySelectorAll(gt),function(t){var n=t.getBoundingClientRect();e.push(Math.round(n.width)+"x"+Math.round(n.height))}),e.join(",")}var Qt=250,Jt=8e3;async function Yt(o){for(var e=Date.now()+(o||Jt),t=Oe();window.componentRegisterIsRenderedResult!==!0;){if(Date.now()>=e)return!1;await mt(100)}if(!t)return!0;for(var n=null;Date.now()<e;){var r=Wt(t);if(r===n)return!0;n=r,await mt(Qt)}return!1}function Gt(o){var e={};return Array.prototype.map.call(o.querySelectorAll(gt),function(t){var n=t.localName,r=e[n]||0;e[n]=r+1;var i=t.getAttribute("id")||t.getAttribute("name");return{id:i||n+"["+r+"]",element:t}})}function Xt(o){var e={};return o.forEach(function(t){var n=t.element,r=n.closest?n.closest("[data-bino-kind]"):null,i={};r&&(i.kind=r.getAttribute("data-bino-kind")||"",i.name=r.getAttribute("data-bino-name")||"",i.ref=r.getAttribute("data-bino-ref")||"");var s=n.getAttribute("measure-unit"),p=n.getAttribute("measure-scale");s&&(i.measureUnit=s),p&&(i.measureScale=p),(i.kind||i.name||i.ref||i.measureUnit)&&(e[t.id]=i)}),e}async function _t(o){var e=Oe();if(!e||typeof e.getLayoutState!="function")return null;var t=await Yt(o&&o.settleTimeoutMs),n=await e.getLayoutState(),r=Gt(e),i=new Map;return r.forEach(function(s){i.set(s.id,s.element)}),{state:n,sources:Xt(r),elements:i,settled:t}}async function yt(o){return!o||typeof o.getLayoutState!="function"?null:o.getLayoutState({detail:"full"})}var ze=class extends z{static properties={artifacts:{type:Array},documents:{type:Array},graph:{type:Object},currentPath:{type:String,attribute:"current-path"},_errorCount:{state:!0},_badgeVisible:{state:!0},_refreshing:{state:!0},_refreshError:{state:!0},_inspectorAvailable:{state:!0}};static styles=L`
    :host {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      z-index: var(--bino-z-toolbar);
      display: flex;
      align-items: center;
      gap: var(--bino-space-md);
      background: var(--bino-surface);
      border-bottom: 1px solid var(--bino-border);
      padding: var(--bino-space-sm) var(--bino-space-md);
      font-size: var(--bino-font-size-md);
      font-family: var(--bino-font-sans);
      box-shadow: var(--bino-shadow-header);
    }
    .title {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-sm);
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    .mark {
      height: 20px;
      width: auto;
      display: block;
    }
    select {
      padding: 0.375rem 0.625rem;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface-subtle);
      font-size: var(--bino-font-size-md);
      color: var(--bino-text-muted);
      cursor: pointer;
      min-width: var(--bino-search-width);
    }
    select:hover {
      border-color: var(--bino-border-hover);
    }
    select:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-focus-ring);
    }
    .warning-badge {
      display: none;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-warning-bg);
      border: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      cursor: pointer;
      user-select: none;
    }
    .warning-badge:hover {
      background: var(--bino-yellow-300);
    }
    .warning-badge.visible {
      display: inline-flex;
    }
    .warning-icon {
      font-size: var(--bino-font-size-md);
    }
    .assets-btn, .graph-btn, .explorer-btn, .inspect-btn, .build-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border-light);
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .assets-btn:hover, .graph-btn:hover:not(:disabled), .explorer-btn:hover, .inspect-btn:hover:not(:disabled), .build-btn:hover:not(:disabled) {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
    .doc-meta {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 32ch;
    }
    .graph-btn:disabled, .present-btn:disabled, .inspect-btn:disabled, .build-btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .present-btn:disabled {
      background: var(--bino-surface);
      border-color: var(--bino-border-light);
      color: var(--bino-text-secondary);
    }
    .assets-icon, .graph-icon, .explorer-icon, .inspect-icon, .build-icon {
      font-size: var(--bino-font-size-md);
    }
    .present-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-accent);
      border: 1px solid var(--bino-accent-strong);
      color: var(--bino-on-accent);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .present-btn:hover:not(:disabled) {
      background: var(--bino-accent-strong);
    }
    .present-icon {
      font-size: var(--bino-font-size-md);
    }
    .spacer {
      flex: 1;
    }
    ::slotted(*) {
      margin-left: auto;
    }
    .progress-bar {
      position: absolute;
      bottom: 0;
      left: 0;
      right: 0;
      height: 2px;
      overflow: hidden;
      opacity: 0;
      transition: opacity 0.15s ease;
    }
    .progress-bar.active {
      opacity: 1;
    }
    .progress-bar::after {
      content: '';
      display: block;
      height: 100%;
      width: 40%;
      background: var(--bino-accent);
      border-radius: 1px;
      animation: progress-slide 1.2s ease-in-out infinite;
    }
    .progress-bar.error {
      opacity: 1;
      height: 3px;
    }
    .progress-bar.error::after {
      width: 100%;
      background: var(--bino-error);
      animation: none;
    }
    @keyframes progress-slide {
      0% { transform: translateX(-100%); }
      100% { transform: translateX(350%); }
    }
    @media (prefers-reduced-motion: reduce) {
      .progress-bar::after {
        animation: none;
        width: 100%;
      }
    }
    .refresh-error-msg {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-error-bg);
      border: 1px solid var(--bino-error-border);
      color: var(--bino-error);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      max-width: 32rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      cursor: help;
    }
  `;constructor(){super(),this.artifacts=[],this.documents=[],this.graph=null,this.currentPath="/",this._errorCount=0,this._badgeVisible=!1,this._refreshing=!1,this._refreshError="",this._panelDismissed=!1,this._inspectorAvailable=!1,this._boundOnContentUpdated=this._refreshInspectorAvailability.bind(this),this._boundOnErrorsChanged=this._onErrorsChanged.bind(this),this._boundOnPanelDismissed=this._onPanelDismissed.bind(this),this._boundOnRefreshing=this._onRefreshing.bind(this),this._boundOnRefreshDone=this._onRefreshDone.bind(this),this._boundOnRefreshError=this._onRefreshError.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.addEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.addEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.addEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.addEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._refreshInspectorAvailability()}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.removeEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.removeEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.removeEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.removeEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated)}render(){var e=this,t=this.currentPath||"/",n=this.artifacts||[],r=[],i=[];n.forEach(function(v){v.isDoc?i.push(v):r.push(v)});var s=t!=="/"&&!t.startsWith("/doc/")&&!t.startsWith("/pres/"),p=s?T()+"/pres"+t:null,a=i.find(function(v){return"/doc/"+v.name===t})||null,h=a||r.find(function(v){return"/"+v.name===t})||null;return d`
      <span class="title">
        <img class="mark" src=${T()+"/__bino/assets/bino-mark.png"} alt="">
        <span>bino preview</span>
      </span>
      <select id="artefact-select" @change=${this._onSelectChange}>
        <option value="/" ?selected=${t==="/"}>All Pages</option>
        ${r.length>0?d`
          <optgroup label="Report Artefacts">
            ${r.map(function(v){var f="/"+v.name,b=v.title?v.title+" ("+v.name+")":v.name;return d`<option value=${f} ?selected=${f===t}>${b}</option>`})}
          </optgroup>
        `:""}
        ${i.length>0?d`
          <optgroup label="Document Artefacts">
            ${i.map(function(v){var f="/doc/"+v.name,b=v.title?v.title+" ("+v.name+")":v.name;return d`<option value=${f} ?selected=${f===t}>${b}</option>`})}
          </optgroup>
        `:""}
      </select>
      ${a?d`
        <span class="doc-meta" title="Document settings from the manifest">${this._docMetaText(a)}</span>
      `:""}
      <span class="warning-badge ${this._badgeVisible?"visible":""}"
        title="Show warnings" @click=${this._onBadgeClick}>
        <span class="warning-icon">\u26A0</span>
        <span>${this._errorCount}</span>
      </span>
      <button class="assets-btn" title="Manifest documents" @click=${this._onAssetsClick}>
        <span class="assets-icon">\u25A6</span>
        <span>Assets (${(this.documents||[]).length})</span>
      </button>
      <button class="graph-btn" ?disabled=${!this.graph}
        title=${this.graph?"Dependency graph":"Dependency graph is only available for a single artefact"}
        @click=${this._onGraphClick}>
        <span class="graph-icon">\u229E</span>
        <span>Graph</span>
      </button>
      <button class="explorer-btn" title="Data Explorer" @click=${this._onExplorerClick}>
        <span class="explorer-icon">\u2636</span>
        <span>Explorer</span>
      </button>
      <button class="inspect-btn" ?disabled=${!this._inspectorAvailable}
        title=${this._inspectorAvailable?a?"Inspect embedded engine components":"Inspect the rendered report":"Requires template engine v1.0.0-next.24 or newer"}
        @click=${this._onInspectClick}>
        <span class="inspect-icon">\u25a3</span>
        <span>Inspect</span>
      </button>
      <button class="present-btn" ?disabled=${!p}
        title=${p?"Open presentation":"Presentation is only available for a report artefact"}
        @click=${function(){p&&window.open(p,"_blank")}}>
        <span class="present-icon">\u25B6</span>
        <span>Present</span>
      </button>
      <button class="build-btn" ?disabled=${!h}
        title=${h?"Build a preview PDF to view or download":"PDF is only available for a single artefact"}
        @click=${function(){e._onPdfClick(h)}}>
        <span class="build-icon">\u2193</span>
        <span>PDF</span>
      </button>
      <span class="spacer"></span>
      ${this._refreshError?d`
        <span class="refresh-error-msg" title=${this._refreshError}>
          <span>⚠</span>
          <span>Refresh failed</span>
        </span>
      `:""}
      <slot></slot>
      <div class="progress-bar ${this._refreshError?"error":this._refreshing?"active":""}"></div>
    `}updated(e){e.has("documents")&&document.dispatchEvent(new CustomEvent("bino-documents-changed",{detail:{documents:this.documents||[]}}))}_onAssetsClick(){document.dispatchEvent(new CustomEvent("bino-open-assets",{detail:{documents:this.documents||[]}}))}_onGraphClick(){document.dispatchEvent(new CustomEvent("bino-open-graph",{detail:{graph:this.graph}}))}_onExplorerClick(){document.dispatchEvent(new CustomEvent("bino-open-explorer"))}_onInspectClick(){document.dispatchEvent(new CustomEvent("bino-open-inspector"))}_docMetaText(e){var t=[];return e.format&&t.push(e.format),e.orientation&&t.push(e.orientation),e.locale&&t.push(e.locale),e.chapters===1&&t.push("1 chapter"),e.chapters>1&&t.push(e.chapters+" chapters"),e.toc&&t.push("TOC"),e.headerFooter&&t.push("header/footer"),t.join(" \xB7 ")}_onPdfClick(e){document.dispatchEvent(new CustomEvent("bino-open-pdf",{detail:{name:e.name,kind:e.isDoc?"DocumentArtefact":"ReportArtefact"}}))}_refreshInspectorAvailability(){this._inspectorAvailable=de()}_onSelectChange(e){var t=e.target.value;t&&(window.location.href=T()+t)}_onBadgeClick(){this._panelDismissed=!1,this._badgeVisible=!1,document.dispatchEvent(new CustomEvent("bino-show-errors"))}_onErrorsChanged(e){this._errorCount=e.detail&&e.detail.count||0,this._panelDismissed&&this._errorCount>0?this._badgeVisible=!0:this._badgeVisible=!1}_onPanelDismissed(){this._panelDismissed=!0,this._errorCount>0&&(this._badgeVisible=!0)}_onRefreshing(){console.debug("bino-toolbar: refreshing \u2192 _refreshing=true"),this._refreshing=!0,this._refreshError=""}_onRefreshDone(){console.debug("bino-toolbar: refresh-done \u2192 _refreshing=false"),this._refreshing=!1}_onRefreshError(e){var t=e&&e.detail&&e.detail.message||"Refresh failed";console.debug("bino-toolbar: refresh-error",t),this._refreshError=String(t),this._refreshing=!1}};customElements.define("bino-toolbar",ze);var Le=class extends z{static properties={_errors:{state:!0},_visible:{state:!0}};static styles=L`
    :host {
      position: fixed;
      bottom: 0;
      left: 0;
      right: 0;
      max-height: var(--bino-panel-max-height);
      overflow-y: auto;
      background: var(--bino-warning-bg);
      border-top: 2px solid var(--bino-warning);
      font-family: var(--bino-font-sans);
      font-size: 13px;
      z-index: var(--bino-z-panel);
      box-shadow: var(--bino-shadow-panel);
      display: none;
    }
    :host([visible]) {
      display: block;
    }
    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 8px 12px;
      background: var(--bino-yellow-300);
      border-bottom: 1px solid var(--bino-warning-border);
      font-weight: 600;
      color: var(--bino-warning-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 18px;
      cursor: pointer;
      color: var(--bino-warning-text);
      padding: 0 4px;
    }
    .close-btn:hover {
      color: var(--bino-gray-900);
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    li {
      padding: 8px 12px;
      border-bottom: 1px solid var(--bino-yellow-300);
      cursor: pointer;
      display: flex;
      align-items: flex-start;
      gap: 8px;
    }
    li:hover {
      background: var(--bino-yellow-300);
    }
    li.highlighted {
      background: var(--bino-yellow-400);
      border-left: 3px solid var(--bino-warning);
    }
    li:last-child {
      border-bottom: none;
    }
    .badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge.warning {
      background: var(--bino-warning-border);
      color: var(--bino-gray-900);
    }
    .badge.error {
      background: var(--bino-error);
      color: #fff;
    }
    .message {
      color: var(--bino-warning-text);
    }
  `;constructor(){super(),this._errors=[],this._visible=!1,this._scanTimer=null,this._observer=null,this._badges=[],this._highlightTimer=null,this._boundOnShowErrors=this._onShowErrors.bind(this),this._boundOnContentUpdated=this._debouncedScan.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-show-errors",this._boundOnShowErrors),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._startObserver(),this._debouncedScan()}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-show-errors",this._boundOnShowErrors),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._observer&&(this._observer.disconnect(),this._observer=null),this._removeBadges()}updated(e){e.has("_visible")&&(this._visible?this.setAttribute("visible",""):this.removeAttribute("visible"))}render(){if(!this._visible||this._errors.length===0)return d``;var e=this,t=this._errors.length;return d`
      <div class="header">
        <span>${t} warning${t!==1?"s":""} found</span>
        <button class="close-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <ul>
        ${this._errors.map(function(n,r){return d`
            <li @click=${()=>e._scrollToElement(n.element)}>
              <span class="badge ${n.error.type||"warning"}">${n.error.type||"warning"}</span>
              <span class="message">${n.error.message||n.error.id||"Unknown error"}</span>
            </li>
          `})}
      </ul>
    `}_startObserver(){var e=this;this._observer=new MutationObserver(function(t){e._onMutation(t)}),this._observer.observe(document.body,{childList:!0,subtree:!0,attributes:!0,attributeFilter:["has-error","has-errors"]})}_onMutation(e){var t=!1;e.forEach(function(n){n.type==="attributes"&&(n.attributeName==="has-error"||n.attributeName==="has-errors")&&(t=!0),n.type==="childList"&&n.addedNodes.length>0&&n.addedNodes.forEach(function(r){r.nodeType===1&&r.hasAttribute&&(r.hasAttribute("has-error")||r.hasAttribute("has-errors"))&&(t=!0),r.nodeType===1&&r.querySelector&&r.querySelector("[has-error], [has-errors]")&&(t=!0)})}),t&&this._debouncedScan()}_debouncedScan(){var e=this;this._scanTimer&&clearTimeout(this._scanTimer),this._scanTimer=setTimeout(function(){e._scanForErrors()},100)}_parseErrors(e){if(!e)return[];try{var t=JSON.parse(e);return Array.isArray(t)?t:[]}catch{return[]}}_scanForErrors(){var e=[],t=document.querySelectorAll("[has-error], [has-errors]"),n=this;t.forEach(function(r){var i=r.getAttribute("has-error")||r.getAttribute("has-errors"),s=n._parseErrors(i);s.forEach(function(p){e.push({element:r,error:p})})}),this._errors=e,document.dispatchEvent(new CustomEvent("bino-errors-changed",{detail:{count:e.length}})),e.length>0?(this._visible=!0,this._injectBadges(e)):(this._visible=!1,this._removeBadges())}_highlightForElement(e){var t=this,n=this.renderRoot.querySelectorAll("li"),r=null;n.forEach(function(i,s){i.classList.remove("highlighted"),t._errors[s]&&t._errors[s].element===e&&(i.classList.add("highlighted"),r||(r=i))}),r&&r.scrollIntoView({block:"nearest",behavior:"smooth"}),this._highlightTimer&&clearTimeout(this._highlightTimer),this._highlightTimer=setTimeout(function(){n.forEach(function(i){i.classList.remove("highlighted")})},4e3)}_onClose(){this._visible=!1,document.dispatchEvent(new CustomEvent("bino-panel-dismissed"))}_onShowErrors(){this._errors.length>0&&(this._visible=!0)}_scrollToElement(e){e&&(e.scrollIntoView({behavior:"smooth",block:"center"}),e.classList.remove("bn-error-highlight"),e.offsetWidth,e.classList.add("bn-error-highlight"),setTimeout(function(){e.classList.remove("bn-error-highlight")},700))}_injectBadges(e){this._removeBadges();var t=this,n=new Map;e.forEach(function(r,i){n.has(r.element)||n.set(r.element,[]),n.get(r.element).push({error:r.error,index:i})}),n.forEach(function(r,i){var s=document.createElement("div");s.className="bn-error-indicator-badge",s.style.cssText="position:absolute;top:2px;right:2px;width:18px;height:18px;background:#fbc02d;color:#11161a;font-size:12px;border-radius:50%;display:flex;align-items:center;justify-content:center;z-index:10000;cursor:pointer;user-select:none;line-height:1;",s.textContent="\u26A0",s.title=r.map(function(h){return h.error.message||h.error.id||"Error"}).join(`
`),s.addEventListener("click",function(h){h.stopPropagation(),t._visible=!0,t._highlightForElement(i)});var p=i.parentNode;if(p){var a=window.getComputedStyle(p);a.position==="static"&&(p.style.position="relative"),i.insertAdjacentElement("afterend",s)}t._badges.push(s)})}_removeBadges(){this._badges.forEach(function(e){e.parentNode&&e.parentNode.removeChild(e)}),this._badges=[]}};customElements.define("bino-error-panel",Le);var Te=class extends z{static properties={_results:{state:!0},_activeIndex:{state:!0},_open:{state:!0}};static styles=L`
    :host {
      position: relative;
      display: inline-block;
      font-family: var(--bino-font-sans);
    }
    .search-wrap {
      position: relative;
      display: flex;
      align-items: center;
    }
    .search-icon {
      position: absolute;
      left: var(--bino-space-sm);
      pointer-events: none;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
      line-height: 1;
    }
    input {
      width: var(--bino-search-width);
      padding: 0.375rem 0.625rem 0.375rem 1.75rem;
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-base);
      font-family: inherit;
      color: var(--bino-text);
      background: var(--bino-surface-subtle);
      transition: width var(--bino-transition-normal), border-color var(--bino-transition-fast), box-shadow var(--bino-transition-fast);
    }
    input:focus {
      width: var(--bino-search-width-focus);
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-focus-ring);
      background: var(--bino-surface);
    }
    input::placeholder {
      color: var(--bino-text-secondary);
    }
    .dropdown {
      display: none;
      position: absolute;
      top: 100%;
      right: 0;
      margin-top: 4px;
      min-width: 320px;
      max-height: 400px;
      overflow-y: auto;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      box-shadow: var(--bino-shadow-dropdown);
      z-index: var(--bino-z-panel);
    }
    .dropdown.open {
      display: block;
    }
    .result {
      display: flex;
      flex-direction: column;
      gap: 2px;
      padding: var(--bino-space-sm) 0.75rem;
      cursor: pointer;
      border-bottom: 1px solid var(--bino-surface-hover);
      font-size: var(--bino-font-size-base);
      transition: background 0.1s;
    }
    .result:last-child {
      border-bottom: none;
    }
    .result:hover, .result.active {
      background: var(--bino-surface-active);
    }
    .result-name {
      font-weight: 500;
      color: var(--bino-text);
    }
    .result-kind {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      text-transform: uppercase;
      letter-spacing: 0.03em;
    }
    .result-context {
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .no-results {
      padding: 0.75rem;
      text-align: center;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text-secondary);
    }
    mark {
      background: var(--bino-yellow-400);
      color: inherit;
      border-radius: 2px;
      padding: 0 1px;
    }
  `;constructor(){super(),this._results=[],this._activeIndex=-1,this._open=!1,this._debounceTimer=null,this._query=""}connectedCallback(){super.connectedCallback();var e=this;this._boundOutsideClick=function(t){!e.contains(t.target)&&!e.renderRoot.contains(t.target)&&e._close()},document.addEventListener("click",this._boundOutsideClick),this._boundContentUpdated=function(){e._query&&e._search(e._query)},document.addEventListener("bn-preview:content-updated",this._boundContentUpdated)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("click",this._boundOutsideClick),document.removeEventListener("bn-preview:content-updated",this._boundContentUpdated)}render(){var e=this,t=ie().startsWith("/doc/")?"Search headings...":"Search elements...";return d`
      <div class="search-wrap">
        <span class="search-icon">\u2315</span>
        <input type="text" placeholder=${t} autocomplete="off" spellcheck="false"
          @input=${this._onInput}
          @keydown=${this._onKeydown}
          @focus=${this._onFocus}>
      </div>
      <div class="dropdown ${this._open?"open":""}">
        ${this._results.length===0&&this._open?d`<div class="no-results">No results found</div>`:this._results.map(function(n,r){return d`
                <div class="result ${r===e._activeIndex?"active":""}"
                  @click=${()=>e._selectResult(r)}>
                  <span class="result-kind">${n.kind}</span>
                  <span class="result-name">${e._highlightMatch(n,e._query)}</span>
                </div>
              `})}
      </div>
    `}_highlightMatch(e,t){if(!t)return e.name;var n=t.toLowerCase(),r=e.name.toLowerCase().indexOf(n);if(r===-1)return e.name;var i=e.name.substring(0,r),s=e.name.substring(r,r+t.length),p=e.name.substring(r+t.length);return d`${i}<mark>${s}</mark>${p}`}_onInput(e){var t=this,n=e.target.value.trim();clearTimeout(this._debounceTimer),this._debounceTimer=setTimeout(function(){t._query=n,t._search(n)},150)}_onKeydown(e){if(e.key==="Escape"){this._close(),e.target.blur();return}if(e.key==="ArrowDown"){e.preventDefault(),this._moveActive(1);return}if(e.key==="ArrowUp"){e.preventDefault(),this._moveActive(-1);return}if(e.key==="Enter"){e.preventDefault(),this._activeIndex>=0&&this._activeIndex<this._results.length?this._selectResult(this._activeIndex):this._results.length>0&&this._selectResult(0);return}}_onFocus(){this._query&&this._results.length>0&&(this._open=!0)}_search(e){if(this._activeIndex=-1,!e||e.length<2){this._results=[],this._close();return}var t=e.toLowerCase(),n=[],r=new Set,i=document.querySelectorAll("[data-bino-kind]");i.forEach(function(a){var h=a.getAttribute("data-bino-kind")||"",v=a.getAttribute("data-bino-name")||"",f=h+":"+v;r.has(f)||(h.toLowerCase().indexOf(t)!==-1||v.toLowerCase().indexOf(t)!==-1)&&(r.add(f),n.push({type:"element",kind:h,name:v,el:a}))});var s=document.querySelectorAll("bn-layout-page[data-bino-page]");s.forEach(function(a){var h=a.getAttribute("data-bino-page")||"",v="page:"+h;r.has(v)||h.toLowerCase().indexOf(t)!==-1&&(r.add(v),n.push({type:"page",kind:"LayoutPage",name:h,el:a}))});var p=document.querySelectorAll(".bn-document-content h1, .bn-document-content h2, .bn-document-content h3, .bn-document-content h4, .bn-document-content h5, .bn-document-content h6");p.forEach(function(a){var h=(a.textContent||"").trim();if(h){var v="heading:"+(a.id||h);r.has(v)||h.toLowerCase().indexOf(t)!==-1&&(r.add(v),n.push({type:"heading",kind:a.tagName.toLowerCase()+" heading",name:h,el:a}))}}),n.length<50&&s.forEach(function(a){for(var h=a.getAttribute("data-bino-page")||"",v=a.shadowRoot||a,f=v.querySelectorAll("*"),b=0;b<f.length&&n.length<50;b++){var y=f[b];if(!(y.tagName==="SCRIPT"||y.tagName==="STYLE"))for(var x=y.childNodes,S=0;S<x.length;S++){var C=x[S];if(C.nodeType===3){var $=C.textContent.trim();if(!(!$||$.length<2)){var u=$.toLowerCase().indexOf(t);if(u!==-1){var _=$.substring(Math.max(0,u-30),Math.min($.length,u+e.length+30)),l="text:"+h+":"+$.substring(u,u+Math.min(40,$.length-u));if(r.has(l))continue;r.add(l),n.push({type:"text",kind:"text in "+h,name:_,el:y,query:e});break}}}}}}),this._results=n,this._open=!0}_moveActive(e){if(this._results.length!==0){var t=this._activeIndex+e;t<0&&(t=this._results.length-1),t>=this._results.length&&(t=0),this._activeIndex=t,this.updateComplete.then(()=>{var n=this.renderRoot.querySelectorAll(".result");n[t]&&n[t].scrollIntoView({block:"nearest"})})}}_selectResult(e){var t=this._results[e];if(!(!t||!t.el)){this._close(),t.el.scrollIntoView({behavior:"smooth",block:"center"});var n=t.el,r=n.style.outline,i=n.style.outlineOffset;n.style.outline="2px solid "+(getComputedStyle(document.documentElement).getPropertyValue("--bino-primary").trim()||"#0b727e"),n.style.outlineOffset="2px",setTimeout(function(){n.style.outline=r,n.style.outlineOffset=i},3e3)}}_close(){this._open=!1,this._activeIndex=-1}};customElements.define("bino-search",Te);var Me=class extends z{static properties={_documents:{state:!0},_open:{state:!0},_selectedDoc:{state:!0},_filterKind:{state:!0}};static styles=L`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: var(--bino-radius-lg);
      box-shadow: var(--bino-shadow-page);
      width: 640px;
      max-width: calc(100vw - 2rem);
      max-height: 80vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover {
      color: var(--bino-text);
    }
    .kind-tabs {
      display: flex;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-wrap: wrap;
      flex-shrink: 0;
    }
    .kind-tab {
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      cursor: pointer;
      font-family: var(--bino-font-sans);
      user-select: none;
    }
    .kind-tab:hover {
      background: var(--bino-surface-hover);
    }
    .kind-tab.active {
      background: var(--bino-surface-active);
      border-color: var(--bino-primary);
      color: var(--bino-active-text);
      font-weight: 600;
    }
    .doc-list {
      flex: 1;
      overflow-y: auto;
      min-height: 0;
    }
    .doc-row {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      cursor: pointer;
    }
    .doc-row:last-child {
      border-bottom: none;
    }
    .doc-row:hover {
      background: var(--bino-surface-hover);
    }
    .doc-row.selected {
      background: var(--bino-surface-active);
    }
    .kind-badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
      background: var(--bino-celeste-100);
      color: var(--bino-celeste-800);
      white-space: nowrap;
    }
    .doc-info {
      min-width: 0;
      flex: 1;
    }
    .doc-name {
      font-weight: 600;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .doc-file {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .detail-panel {
      border-top: 2px solid var(--bino-border);
      padding: var(--bino-space-md) var(--bino-space-lg);
      flex-shrink: 0;
      background: var(--bino-surface-subtle);
    }
    .detail-row {
      display: flex;
      gap: var(--bino-space-sm);
      margin-bottom: var(--bino-space-xs);
      font-size: var(--bino-font-size-sm);
      align-items: baseline;
    }
    .detail-row:last-child {
      margin-bottom: 0;
    }
    .detail-label {
      color: var(--bino-text-secondary);
      font-weight: 600;
      flex-shrink: 0;
      min-width: 80px;
    }
    .detail-value {
      color: var(--bino-text);
      min-width: 0;
      word-break: break-word;
    }
    .pills {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs);
    }
    .pill {
      padding: 1px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      background: var(--bino-gray-200);
      color: var(--bino-text-muted);
    }
    .pill.label-pill {
      background: var(--bino-gray-100);
      color: var(--bino-gray-700);
    }
    .pill.constraint-pill {
      background: var(--bino-yellow-200);
      color: var(--bino-gray-800);
    }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;constructor(){super(),this._documents=[],this._open=!1,this._selectedDoc=null,this._filterKind="",this._boundOnOpen=this._onOpen.bind(this),this._boundOnChanged=this._onChanged.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-assets",this._boundOnOpen),document.addEventListener("bino-documents-changed",this._boundOnChanged),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-assets",this._boundOnOpen),document.removeEventListener("bino-documents-changed",this._boundOnChanged),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return w;var e=this,t=this._filteredDocs(),n=this._uniqueKinds();return d`
      <div class="backdrop" @click=${this._onBackdropClick}>
        <div class="modal" @click=${this._stopPropagation}>
          <div class="modal-header">
            <h2>Manifest Documents</h2>
            <button class="close-btn" title="Close" @click=${this._close}>&times;</button>
          </div>
          <div class="kind-tabs">
            <button class="kind-tab ${this._filterKind===""?"active":""}"
              @click=${function(){e._filterKind="",e._selectedDoc=null}}>
              All (${this._documents.length})
            </button>
            ${n.map(function(r){var i=e._countKind(r);return d`
                <button class="kind-tab ${e._filterKind===r?"active":""}"
                  @click=${function(){e._filterKind=r,e._selectedDoc=null}}>
                  ${r} (${i})
                </button>
              `})}
          </div>
          <div class="doc-list">
            ${t.length===0?d`<div class="empty">No documents found</div>`:t.map(function(r){var i=e._selectedDoc===r;return d`
                    <div class="doc-row ${i?"selected":""}"
                      @click=${function(){e._selectedDoc=i?null:r}}>
                      <span class="kind-badge">${r.kind}</span>
                      <div class="doc-info">
                        <div class="doc-name">${r.name}</div>
                        <div class="doc-file">${r.file}</div>
                      </div>
                    </div>
                  `})}
          </div>
          ${this._selectedDoc?this._renderDetail(this._selectedDoc):w}
        </div>
      </div>
    `}_renderDetail(e){var t=e.labels||{},n=Object.keys(t),r=e.constraints||[];return d`
      <div class="detail-panel">
        <div class="detail-row">
          <span class="detail-label">Kind</span>
          <span class="detail-value">${e.kind}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">Name</span>
          <span class="detail-value">${e.name}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">File</span>
          <span class="detail-value">${e.file}</span>
        </div>
        ${n.length>0?d`
          <div class="detail-row">
            <span class="detail-label">Labels</span>
            <div class="pills">
              ${n.map(function(i){return d`<span class="pill label-pill">${i}: ${t[i]}</span>`})}
            </div>
          </div>
        `:w}
        ${r.length>0?d`
          <div class="detail-row">
            <span class="detail-label">Constraints</span>
            <div class="pills">
              ${r.map(function(i){return d`<span class="pill constraint-pill">${i}</span>`})}
            </div>
          </div>
        `:w}
      </div>
    `}_filteredDocs(){if(!this._filterKind)return this._documents;var e=this._filterKind;return this._documents.filter(function(t){return t.kind===e})}_uniqueKinds(){var e={},t=[];return this._documents.forEach(function(n){e[n.kind]||(e[n.kind]=!0,t.push(n.kind))}),t}_countKind(e){var t=0;return this._documents.forEach(function(n){n.kind===e&&t++}),t}_onOpen(e){this._documents=e.detail&&e.detail.documents||[],this._open=!0,this._selectedDoc=null,this._filterKind=""}_onChanged(e){if(this._open&&(this._documents=e.detail&&e.detail.documents||[],this._selectedDoc)){var t=this._selectedDoc,n=this._documents.some(function(r){return r.kind===t.kind&&r.name===t.name&&r.file===t.file});n||(this._selectedDoc=null)}}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_onBackdropClick(){this._close()}_stopPropagation(e){e.stopPropagation()}_close(){this._open=!1,this._selectedDoc=null}};customElements.define("bino-assets-modal",Me);var K=170,te=38,De=20,Zt=56,ce=30,en={ReportArtefact:{bg:"#d4f7f9",stroke:"#0b727e",text:"#0c454c"},DocumentArtefact:{bg:"#d4f7f9",stroke:"#0b727e",text:"#0c454c"},LayoutPage:{bg:"#d6ecdd",stroke:"#1f7a3d",text:"#1f7a3d"},LayoutCard:{bg:"#ecfcfd",stroke:"#5cdae5",text:"#0c5a64"},Component:{bg:"#f7aace",stroke:"#e23e8c",text:"#c11f6e"},DataSet:{bg:"#fff0a8",stroke:"#d99e0b",text:"#1f262a"},DataSource:{bg:"#f6ddda",stroke:"#c0392b",text:"#c0392b"},MarkdownFile:{bg:"#eef2f3",stroke:"#9aa7ad",text:"#333c41"}},tn={ReportArtefact:"Artefact",DocumentArtefact:"DocArtefact",LayoutPage:"Page",LayoutCard:"Card",Component:"Component",DataSet:"DataSet",DataSource:"Source",MarkdownFile:"Markdown"};function xt(o){return en[o]||{bg:"#eef2f3",stroke:"#9aa7ad",text:"#333c41"}}function nn(o){return tn[o]||o}function rn(o,e){return e||(e=20),!o||o.length<=e?o||"":o.substring(0,e-1)+"\u2026"}function on(o){return o==="auto"||o.indexOf("inherited-")===0||o.indexOf("${")===0}function sn(o){for(var e={},t=0,n=0;n<o.length;n++)e[o[n].column]||(e[o[n].column]=!0,t++);return t}function an(o){for(var e=[],t=null,n=null,r=0;r<o.length;r++){var i=o[r];(!t||t.name!==i.dataset)&&(t={name:i.dataset,groups:[]},e.push(t),n=null),(!n||n.role!==i.role)&&(n={role:i.role,columns:[]},t.groups.push(n)),n.columns.indexOf(i.column)===-1&&n.columns.push(i.column)}return e}function ln(o){if(!o||!o.rootId)return null;var e=o.nodes||{},t={},n={};function r(i){if(!i)return null;var s=e[i];if(!s)return null;if(t[i])return{id:i,kind:s.kind,name:s.name||i,children:[],cycle:!0};if(n[i])return{id:i,kind:s.kind,name:s.name||i,children:[],ref:!0};t[i]=!0,n[i]=!0;for(var p=[],a=s.dependsOn||[],h=0;h<a.length;h++){var v=r(a[h]);v&&p.push(v)}return t[i]=!1,{id:i,kind:s.kind,name:s.name||i,children:p}}return r(o.rootId)}function wt(o){if(!o)return 0;if(o.children.length===0)return o.w=K,K;for(var e=0,t=0;t<o.children.length;t++)e+=wt(o.children[t]);return e+=(o.children.length-1)*De,o.w=Math.max(e,K),o.w}function dn(o){if(!o)return{nodes:[],edges:[],width:0,height:0};wt(o);var e=[],t=[],n=0,r=0;function i(s,p,a){if(s){var h=p-K/2;if(e.push({id:s.id,kind:s.kind,name:s.name,x:h,y:a,cx:p,ref:s.ref||!1,cycle:s.cycle||!1}),p+K/2>n&&(n=p+K/2),a+te>r&&(r=a+te),s.children.length!==0){for(var v=a+te+Zt,f=0,b=0;b<s.children.length;b++)f+=s.children[b].w;f+=(s.children.length-1)*De;for(var y=p-f/2,b=0;b<s.children.length;b++){var x=s.children[b],S=y+x.w/2;t.push({x1:p,y1:a+te,x2:S,y2:v}),i(x,S,v),y+=x.w+De}}}}return i(o,o.w/2+ce,ce),{nodes:e,edges:t,width:n+ce,height:r+ce}}var Re=class extends z{static properties={_graphData:{state:!0},_open:{state:!0},_selected:{state:!0}};static styles=L`
    :host { font-family: var(--bino-font-sans); }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: var(--bino-radius-lg);
      box-shadow: var(--bino-shadow-page);
      width: 90vw;
      max-width: 1200px;
      max-height: 85vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover { color: var(--bino-text); }
    .graph-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
      background: var(--bino-surface-subtle);
      padding: var(--bino-space-md);
    }
    svg { display: block; }
    svg text { font-family: var(--bino-font-sans); }
    .legend {
      display: flex;
      gap: var(--bino-space-md);
      flex-wrap: wrap;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .legend-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .legend-swatch {
      width: 12px;
      height: 12px;
      border-radius: 3px;
    }
    .columns {
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
      max-height: 30vh;
      overflow: auto;
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
    }
    .columns-title {
      font-weight: 600;
      margin-bottom: var(--bino-space-xs);
    }
    .columns-row {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs) var(--bino-space-md);
    }
    .columns-dataset { font-weight: 600; }
    .columns-role { color: var(--bino-text-secondary); }
    .columns .muted { color: var(--bino-text-muted); font-style: italic; }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;constructor(){super(),this._graphData=null,this._open=!1,this._selected=null,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-graph",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-graph",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return w;var e=ln(this._graphData),t=dn(e);return d`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>Dependency Graph</h2>
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          <div class='graph-container'>
            ${this._renderSVG(t)}
          </div>
          ${this._renderColumns()}
          ${this._renderLegend(t)}
        </div>
      </div>
    `}_renderSVG(e){if(!e||e.nodes.length===0)return d`<div class='empty'>No graph data available</div>`;var t=this,n=e.width,r=e.height,i=this._graphData&&this._graphData.nodes||{},s=e.edges.map(function(a){var h=(a.y1+a.y2)/2;return Ae`<path d=${"M"+a.x1+","+a.y1+" C"+a.x1+","+h+" "+a.x2+","+h+" "+a.x2+","+a.y2}
        fill='none' stroke='#cbd4d8' stroke-width='1.5'/>`}),p=e.nodes.map(function(a){var h=xt(a.kind),v=a.ref||a.cycle?"0.5":"1",f=i[a.id]&&i[a.id].columns||[],b=nn(a.kind);if(f.length>0){var y=sn(f);b+=" \xB7 "+y+(y===1?" col":" cols")}var x=rn(a.name),S=a.cycle?" [cycle]":a.ref?" [ref]":"";return Ae`
        <g opacity=${v} style=${f.length>0?"cursor: pointer":w}
          @click=${f.length>0?function(){t._selected=a.id}:w}>
          <rect x=${a.x} y=${a.y} width=${K} height=${te}
            rx='6' fill=${h.bg} stroke=${h.stroke} stroke-width=${a.id===t._selected?"3":"1.5"}/>
          <text x=${a.cx} y=${a.y+14} text-anchor='middle'
            font-size='10' font-weight='600' fill=${h.stroke}>${b}</text>
          <text x=${a.cx} y=${a.y+28} text-anchor='middle'
            font-size='11' fill=${h.text}>${x}${S}</text>
        </g>
      `});return d`
      <svg width=${n} height=${r} viewBox=${"0 0 "+n+" "+r}>
        ${s}
        ${p}
      </svg>
    `}_renderColumns(){var e=this._graphData&&this._graphData.nodes||{},t=e[this._selected];return!t||!t.columns?w:d`
      <div class='columns'>
        <div class='columns-title'>${t.name||t.id}</div>
        ${an(t.columns).map(function(n){return d`
            <div class='columns-row'>
              <span class='columns-dataset'>${n.name}</span>
              ${n.groups.map(function(r){return d`<span><span class='columns-role'>${r.role}:</span> ${r.columns.map(function(i,s){return d`${s>0?", ":""}<span class=${on(i)?"muted":""}>${i}</span>`})}</span>`})}
            </div>
          `})}
      </div>
    `}_renderLegend(e){if(!e||e.nodes.length===0)return w;var t={},n=[];return e.nodes.forEach(function(r){t[r.kind]||(t[r.kind]=!0,n.push(r.kind))}),n.length===0?w:d`
      <div class='legend'>
        ${n.map(function(r){var i=xt(r);return d`
            <span class='legend-item'>
              <span class='legend-swatch' style=${"background:"+i.bg+";border:1.5px solid "+i.stroke}></span>
              ${r}
            </span>
          `})}
      </div>
    `}_onOpen(e){this._graphData=e.detail&&e.detail.graph||null,this._selected=null,this._open=!0}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_close(){this._open=!1}_stop(e){e.stopPropagation()}};customElements.define("bino-graph-modal",Re);function cn(){var o=window.location.ancestorOrigins;if(!o)return!1;for(var e=0;e<o.length;e++)if(o[e].indexOf("vscode-webview:")===0)return!0;return!1}function un(o){for(var e=atob(o),t=new Uint8Array(e.length),n=0;n<e.length;n++)t[n]=e.charCodeAt(n);return new Blob([t],{type:"application/pdf"})}var Pe=class extends z{static properties={_open:{state:!0},_state:{state:!0},_url:{state:!0},_error:{state:!0},_warnings:{state:!0}};static styles=L`
    :host { font-family: var(--bino-font-sans); }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: var(--bino-radius-lg);
      box-shadow: var(--bino-shadow-page);
      width: 90vw;
      max-width: 1200px;
      height: 85vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      gap: var(--bino-space-md);
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      flex: 1;
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .download-btn {
      padding: 4px 12px;
      border-radius: var(--bino-radius);
      background: var(--bino-accent);
      border: 1px solid var(--bino-accent-strong);
      color: var(--bino-on-accent);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      text-decoration: none;
    }
    .download-btn:hover { background: var(--bino-accent-strong); }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover { color: var(--bino-text); }
    .warnings {
      flex-shrink: 0;
      max-height: 30%;
      overflow: auto;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      font-size: var(--bino-font-size-sm);
    }
    .warnings summary { cursor: pointer; font-weight: 600; }
    .warnings li { white-space: pre-wrap; overflow-wrap: anywhere; }
    .viewer {
      flex: 1;
      min-height: 0;
      border: 0;
      background: var(--bino-surface-subtle);
    }
    .message {
      flex: 1;
      min-height: 0;
      overflow: auto;
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
    .message a { color: var(--bino-primary); overflow-wrap: anywhere; }
    .error {
      margin: 0;
      padding: var(--bino-space-md);
      border: 1px solid var(--bino-error-border);
      border-radius: var(--bino-radius);
      background: var(--bino-error-bg);
      color: var(--bino-text);
      font: var(--bino-font-size-sm) var(--bino-font-mono);
      text-align: left;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    .footer {
      flex-shrink: 0;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
  `;constructor(){super(),this._open=!1,this._state="building",this._url="",this._error="",this._warnings=[],this._name="",this._controller=null,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-pdf",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-pdf",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown),this._reset()}render(){return this._open?d`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>PDF preview: ${this._name}</h2>
            ${this._state==="ready"?d`
              <a class='download-btn' href=${this._url}
                download=${this._name.replace(/[@/]/g,"_")+"-preview.pdf"}>Download</a>
            `:""}
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          ${this._renderWarnings()}
          ${this._renderBody()}
          ${this._state==="ready"?d`
            <div class='footer'>
              Every page is marked PREVIEW. For the final PDF run: bino build --artefact ${this._name}
            </div>
          `:""}
        </div>
      </div>
    `:w}_renderWarnings(){var e=this._warnings.length;return e===0?"":d`
      <details class='warnings' ?open=${this._state==="error"}>
        <summary>${e} warning${e!==1?"s":""}. The PDF may be incomplete.</summary>
        <ul>${this._warnings.map(function(t){return d`<li>${t}</li>`})}</ul>
      </details>
    `}_renderBody(){return this._state==="embedded"?d`
        <div class='message'>
          <p>The PDF preview does not work inside VS Code. Open the preview in a browser:</p>
          <p><a href=${window.location.href} target='_blank' rel='noopener'>${window.location.href}</a></p>
        </div>
      `:this._state==="error"?d`
        <div class='message'>
          <p>The PDF could not be built.</p>
          <pre class='error'>${this._error}</pre>
        </div>
      `:this._state==="building"?d`<div class='message'>Building the PDF… This can take a minute.</div>`:navigator.pdfViewerEnabled===!1?d`<div class='message'>Your browser does not show PDFs inline. Use Download.</div>`:d`<iframe class='viewer' title='PDF preview' src=${this._url}></iframe>`}_onOpen(e){if(!this._open){if(this._reset(),this._name=e.detail.name,this._open=!0,cn()){this._state="embedded";return}this._build(e.detail.kind)}}_build(e){var t=this,n=new AbortController;this._controller=n,this._state="building";var r=T()+"/__preview/pdf?kind="+encodeURIComponent(e)+"&name="+encodeURIComponent(this._name);fetch(r,{method:"POST",signal:n.signal}).then(function(i){return i.json().catch(function(){return{}}).then(function(s){if(n===t._controller){if(t._warnings=s.warnings||[],!s.pdf)throw new Error(s.error||"HTTP "+i.status);t._url=URL.createObjectURL(un(s.pdf)),t._state="ready"}})}).catch(function(i){n===t._controller&&(t._error=i.message||String(i),t._state="error")})}_reset(){this._controller&&(this._controller.abort(),this._controller=null),this._url&&(URL.revokeObjectURL(this._url),this._url=""),this._error="",this._warnings=[]}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_close(){this._reset(),this._open=!1}_stop(e){e.stopPropagation()}};customElements.define("bino-pdf-modal",Pe);var hn=[25,50,100,250],$t=20,kt="bino-explorer-layout",ue="bino-explorer-sql",Ne="bino-explorer-history",he=180,pe=560,ne=64,pn=140;function j(o,e,t){return Math.min(Math.max(o,e),t)}function re(o,e){try{var t=window.localStorage.getItem(o);return t!=null?JSON.parse(t):e}catch{return e}}function be(o,e){try{window.localStorage.setItem(o,JSON.stringify(e))}catch{}}var Ie=class extends z{static properties={_open:{state:!0},_metadata:{state:!0},_sql:{state:!0},_result:{state:!0},_summarizeResult:{state:!0},_loading:{state:!0},_error:{state:!0},_page:{state:!0},_pageSize:{state:!0},_activeTab:{state:!0},_expandedSource:{state:!0},_refreshing:{state:!0},_sidebarWidth:{state:!0},_editorHeight:{state:!0},_dragging:{state:!0},_history:{state:!0},_historyOpen:{state:!0},_exporting:{state:!0}};static styles=L`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      flex-direction: column;
    }
    .explorer {
      display: flex;
      flex-direction: column;
      width: 100%;
      height: 100%;
      background: var(--bino-surface);
    }
    .explorer.dragging {
      user-select: none;
      -webkit-user-select: none;
    }
    .explorer-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: var(--bino-surface);
    }
    .explorer-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .header-actions {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
    }
    .refresh-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 4px 10px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-secondary);
    }
    .refresh-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
      color: var(--bino-text);
    }
    .refresh-btn.refreshing {
      opacity: 0.6;
      cursor: not-allowed;
    }
    .refresh-icon {
      display: inline-block;
      font-size: var(--bino-font-size-md);
      line-height: 1;
    }
    .refresh-btn.refreshing .refresh-icon {
      animation: spin 0.8s linear infinite;
    }
    @keyframes spin {
      to { transform: rotate(360deg); }
    }
    @media (prefers-reduced-motion: reduce) {
      .refresh-btn.refreshing .refresh-icon {
        animation: none;
      }
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover {
      color: var(--bino-text);
    }
    .explorer-body {
      display: flex;
      flex: 1;
      min-height: 0;
      overflow: hidden;
    }
    .sidebar {
      border-right: none;
      overflow-y: auto;
      background: var(--bino-surface-subtle);
      flex-shrink: 0;
    }
    .splitter-v {
      width: 5px;
      flex-shrink: 0;
      cursor: col-resize;
      background: var(--bino-border);
      transition: background 0.15s;
      touch-action: none;
    }
    .splitter-v:hover,
    .splitter-v:focus-visible,
    .splitter-v.active {
      background: var(--bino-primary);
      outline: none;
    }
    .splitter-h {
      height: 5px;
      flex-shrink: 0;
      cursor: row-resize;
      background: var(--bino-border);
      transition: background 0.15s;
      touch-action: none;
    }
    .splitter-h:hover,
    .splitter-h:focus-visible,
    .splitter-h.active {
      background: var(--bino-primary);
      outline: none;
    }
    .sidebar-section {
      padding: var(--bino-space-sm) 0;
    }
    .sidebar-title {
      padding: var(--bino-space-xs) var(--bino-space-md);
      font-size: var(--bino-font-size-xs);
      font-weight: 700;
      text-transform: uppercase;
      color: var(--bino-primary);
      letter-spacing: 0.05em;
    }
    .sidebar-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 5px var(--bino-space-md);
      cursor: pointer;
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
      border-left: 3px solid transparent;
    }
    .sidebar-item:hover {
      background: var(--bino-surface-hover);
      border-left-color: var(--bino-border-light);
    }
    .sidebar-item-name {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-weight: 500;
    }
    .sidebar-item-badge {
      flex-shrink: 0;
      padding: 1px 5px;
      border-radius: 3px;
      font-size: 10px;
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge-source {
      background: var(--bino-bad-soft);
      color: var(--bino-bad);
    }
    .badge-dataset {
      background: var(--bino-yellow-200);
      color: var(--bino-gray-800);
    }
    .sidebar-info-btn {
      flex-shrink: 0;
      background: none;
      border: none;
      cursor: pointer;
      color: var(--bino-text-secondary);
      font-size: 14px;
      padding: 0 2px;
      line-height: 1;
    }
    .sidebar-info-btn:hover {
      color: var(--bino-primary);
    }
    .column-list {
      padding: 2px var(--bino-space-md) var(--bino-space-xs) calc(var(--bino-space-md) + 12px);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .column-entry {
      display: flex;
      justify-content: space-between;
      padding: 1px 0;
    }
    .column-name {
      font-weight: 500;
      color: var(--bino-text-muted);
    }
    .column-type {
      font-style: italic;
      color: var(--bino-text-secondary);
    }
    .main-panel {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-width: 0;
      overflow: hidden;
    }
    .editor-area {
      display: flex;
      flex-direction: column;
      flex-shrink: 0;
      min-height: 0;
    }
    .sql-editor {
      width: 100%;
      flex: 1;
      min-height: 0;
      padding: var(--bino-space-sm) var(--bino-space-md);
      border: none;
      outline: none;
      resize: none;
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-sm);
      line-height: 1.5;
      background: var(--bino-gray-900);
      color: var(--bino-gray-100);
      box-sizing: border-box;
    }
    .sql-editor::placeholder {
      color: var(--bino-gray-500);
    }
    .editor-toolbar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      background: var(--bino-surface-inset);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .editor-btn {
      padding: 4px 12px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-muted);
    }
    .editor-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
    .editor-btn.primary {
      background: var(--bino-accent);
      border-color: var(--bino-accent-strong);
      color: var(--bino-on-accent);
    }
    .editor-btn.primary:hover {
      background: var(--bino-accent-strong);
    }
    .editor-btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .editor-shortcut {
      font-size: 10px;
      color: var(--bino-text-secondary);
      margin-left: auto;
    }
    .history-wrap {
      position: relative;
    }
    .history-menu {
      position: absolute;
      top: calc(100% + 4px);
      left: 0;
      z-index: 10;
      min-width: 320px;
      max-width: 520px;
      max-height: 320px;
      overflow-y: auto;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      box-shadow: var(--bino-shadow-dropdown);
    }
    .history-item {
      display: flex;
      align-items: baseline;
      gap: var(--bino-space-sm);
      width: 100%;
      padding: 6px var(--bino-space-md);
      border: none;
      border-bottom: 1px solid var(--bino-border);
      background: none;
      cursor: pointer;
      text-align: left;
      font-family: var(--bino-font-sans);
    }
    .history-item:last-child {
      border-bottom: none;
    }
    .history-item:hover {
      background: var(--bino-surface-hover);
    }
    .history-sql {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text);
    }
    .history-time {
      flex-shrink: 0;
      font-size: 10px;
      color: var(--bino-text-secondary);
    }
    .history-empty {
      padding: var(--bino-space-md);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      text-align: center;
    }
    .results-area {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-height: 0;
      overflow: hidden;
    }
    .tab-bar {
      display: flex;
      gap: 0;
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: var(--bino-surface-subtle);
    }
    .tab-btn {
      padding: var(--bino-space-xs) var(--bino-space-md);
      border: none;
      background: none;
      font-size: var(--bino-font-size-sm);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-secondary);
      cursor: pointer;
      border-bottom: 2px solid transparent;
      font-weight: 500;
    }
    .tab-btn:hover {
      color: var(--bino-text);
      background: var(--bino-surface-hover);
    }
    .tab-btn.active {
      color: var(--bino-primary);
      border-bottom-color: var(--bino-primary);
      font-weight: 600;
    }
    .table-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: var(--bino-font-size-sm);
    }
    thead {
      position: sticky;
      top: 0;
      z-index: 1;
    }
    th {
      background: var(--bino-surface-inset);
      padding: 6px 12px;
      text-align: left;
      font-weight: 600;
      color: var(--bino-text-muted);
      border-bottom: 2px solid var(--bino-border);
      white-space: nowrap;
      font-size: var(--bino-font-size-xs);
    }
    .col-type {
      font-weight: 400;
      color: var(--bino-text-secondary);
      font-style: italic;
      margin-left: 4px;
    }
    td {
      padding: 4px 12px;
      border-bottom: 1px solid var(--bino-border);
      max-width: 300px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      color: var(--bino-text);
    }
    .row-num {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
      text-align: right;
      width: 1%;
      user-select: none;
    }
    tr:nth-child(even) {
      background: var(--bino-surface-subtle);
    }
    tr:hover {
      background: var(--bino-surface-hover);
    }
    .pagination {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: var(--bino-surface-subtle);
    }
    .pagination button {
      padding: 2px 8px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      cursor: pointer;
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
    }
    .pagination button:hover:not(:disabled) {
      background: var(--bino-surface-hover);
    }
    .pagination button:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }
    .pagination select {
      padding: 2px 4px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
      background: var(--bino-surface);
    }
    .status-bar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: var(--bino-surface-subtle);
      flex-shrink: 0;
    }
    .error-msg {
      padding: var(--bino-space-md);
      color: var(--bino-error);
      font-size: var(--bino-font-size-sm);
      white-space: pre-wrap;
      word-break: break-word;
    }
    .loading-msg {
      padding: var(--bino-space-lg);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
    }
    .empty-state {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
    .duration {
      color: var(--bino-text-secondary);
    }
  `;constructor(){super(),this._open=!1,this._metadata=null,this._sql=typeof re(ue,"")=="string"?re(ue,""):"",this._result=null,this._summarizeResult=null,this._loading=!1,this._error="",this._page=0,this._pageSize=50,this._activeTab="results",this._expandedSource=null,this._refreshing=!1;var e=re(kt,{});this._sidebarWidth=j(Number(e.sidebarWidth)||280,he,pe),this._editorHeight=Math.max(Number(e.editorHeight)||160,ne),this._dragging=!1,this._history=Array.isArray(re(Ne,[]))?re(Ne,[]):[],this._historyOpen=!1,this._exporting=!1,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this),this._boundOnDocsChanged=this._onDocsChanged.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-explorer",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown),document.addEventListener("bino-documents-changed",this._boundOnDocsChanged)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-explorer",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown),document.removeEventListener("bino-documents-changed",this._boundOnDocsChanged)}render(){return this._open?d`
      <div class="backdrop">
        <div class="explorer ${this._dragging?"dragging":""}" @click=${this._onExplorerClick}>
          <div class="explorer-header">
            <h2>Data Explorer</h2>
            <div class="header-actions">
              <button class="refresh-btn ${this._refreshing?"refreshing":""}"
                title="Refresh data sources and datasets"
                ?disabled=${this._refreshing}
                @click=${this._onRefresh}>
                <span class="refresh-icon">↻</span>
                <span>${this._refreshing?"Refreshing...":"Refresh"}</span>
              </button>
              <button class="close-btn" title="Close (Esc)" @click=${this._close}>&times;</button>
            </div>
          </div>
          <div class="explorer-body">
            ${this._renderSidebar()}
            <div class="splitter-v ${this._dragging==="sidebar"?"active":""}"
              role="separator" aria-orientation="vertical" aria-label="Resize sidebar" tabindex="0"
              title="Drag to resize the sidebar (arrow keys when focused)"
              @pointerdown=${this._startSidebarDrag}
              @keydown=${this._onSidebarSplitterKey}></div>
            ${this._renderMainPanel()}
          </div>
        </div>
      </div>
    `:w}_renderSidebar(){var e=this,t=this._metadata,n="width: "+this._sidebarWidth+"px; min-width: "+this._sidebarWidth+"px;";return t?d`
      <div class="sidebar" style=${n}>
        ${t.sources&&t.sources.length>0?d`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSources (${t.sources.length})</div>
            ${t.sources.map(function(r){var i=e._expandedSource===r.name;return d`
                <div>
                  <div class="sidebar-item" @click=${function(){e._selectSource(r.name)}}>
                    <span class="sidebar-item-name">${r.name}</span>
                    ${r.type?d`<span class="sidebar-item-badge badge-source">${r.type}</span>`:w}
                    <button class="sidebar-info-btn" title="Show columns"
                      @click=${function(s){s.stopPropagation(),e._toggleSourceInfo(r.name)}}>
                      ${i?"\u25B4":"\u25BE"}
                    </button>
                  </div>
                  ${i&&r.columns&&r.columns.length>0?d`
                    <div class="column-list">
                      ${r.columns.map(function(s){return d`
                          <div class="column-entry">
                            <span class="column-name">${s.name}</span>
                            <span class="column-type">${s.type}</span>
                          </div>
                        `})}
                    </div>
                  `:w}
                </div>
              `})}
          </div>
        `:w}
        ${t.datasets&&t.datasets.length>0?d`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSets (${t.datasets.length})</div>
            ${t.datasets.map(function(r){return d`
                <div class="sidebar-item" @click=${function(){e._selectDataset(r)}}
                     title=${r.sqlError?"cannot resolve dataset SQL: "+r.sqlError:r.sql||""}>
                  <span class="sidebar-item-name">${r.name}</span>
                  <span class="sidebar-item-badge badge-dataset">set</span>
                </div>
              `})}
          </div>
        `:w}
        ${(!t.sources||t.sources.length===0)&&(!t.datasets||t.datasets.length===0)?d`<div class="empty-state">No data sources found</div>`:w}
      </div>
    `:d`<div class="sidebar" style=${n}><div class="loading-msg">Loading...</div></div>`}_renderMainPanel(){var e=this;return d`
      <div class="main-panel">
        <div class="editor-area" style="height: ${this._editorHeight}px;">
          <textarea class="sql-editor"
            placeholder="Enter SQL query... (Ctrl+Enter to run)"
            .value=${this._sql}
            @input=${this._onSqlInput.bind(this)}
            @keydown=${this._onEditorKeydown.bind(this)}
            spellcheck="false"
          ></textarea>
          <div class="editor-toolbar">
            <button class="editor-btn primary" ?disabled=${this._loading} @click=${this._runQuery.bind(this)}>Run</button>
            <button class="editor-btn" ?disabled=${this._loading||!this._sql.trim()} @click=${this._runSummarize.bind(this)}>Summarize</button>
            <div class="history-wrap">
              <button class="editor-btn" title="Recent queries"
                @click=${function(t){t.stopPropagation(),e._historyOpen=!e._historyOpen}}>History ▾</button>
              ${this._historyOpen?this._renderHistoryMenu():w}
            </div>
            <button class="editor-btn" ?disabled=${this._exporting||!this._sql.trim()}
              title="Download the full result set as CSV"
              @click=${this._exportCSV.bind(this)}>${this._exporting?"Exporting...":"Export CSV"}</button>
            <button class="editor-btn" @click=${this._clearEditor.bind(this)}>Clear</button>
            <span class="editor-shortcut">${navigator.platform.includes("Mac")?"\u2318":"Ctrl"}+Enter to run</span>
          </div>
        </div>
        <div class="splitter-h ${this._dragging==="editor"?"active":""}"
          role="separator" aria-orientation="horizontal" aria-label="Resize SQL editor" tabindex="0"
          title="Drag to resize the SQL editor (arrow keys when focused)"
          @pointerdown=${this._startEditorDrag}
          @keydown=${this._onEditorSplitterKey}></div>
        <div class="results-area">
          <div class="tab-bar">
            <button class="tab-btn ${this._activeTab==="results"?"active":""}"
              @click=${function(){e._activeTab="results"}}>Results</button>
            <button class="tab-btn ${this._activeTab==="summarize"?"active":""}"
              @click=${function(){e._activeTab="summarize"}}>Summarize</button>
          </div>
          ${this._activeTab==="results"?this._renderResults():this._renderSummarizeTab()}
        </div>
      </div>
    `}_renderHistoryMenu(){var e=this;return this._history.length?d`
      <div class="history-menu" @click=${function(t){t.stopPropagation()}}>
        ${this._history.map(function(t){return d`
            <button class="history-item" title=${t.sql}
              @click=${function(){e._applyHistoryEntry(t)}}>
              <span class="history-sql">${t.sql}</span>
              <span class="history-time">${e._formatHistoryTime(t.ts)}</span>
            </button>
          `})}
      </div>
    `:d`<div class="history-menu"><div class="history-empty">No queries yet</div></div>`}_renderResults(){if(this._loading)return d`<div class="loading-msg">Executing query...</div>`;if(this._error)return d`<div class="error-msg">${this._error}</div>`;if(!this._result)return d`<div class="empty-state">Run a query to see results</div>`;if(this._result.error)return d`
        <div class="error-msg">${this._result.error}</div>
        ${this._result.durationMs!=null?d`<div class="status-bar"><span class="duration">${this._result.durationMs}ms</span></div>`:w}
      `;var e=this,t=this._result.columns||[],n=this._result.rows||[],r=this._result.totalRows,i=typeof r=="number"?Math.ceil(r/this._pageSize):null,s=this._page*this._pageSize;return d`
      <div class="table-container">
        ${t.length===0?d`<div class="empty-state">No columns returned</div>`:d`
            <table>
              <thead>
                <tr>
                  <th class="row-num">#</th>
                  ${t.map(function(p){return d`<th>${p.name}<span class="col-type">${p.type}</span></th>`})}
                </tr>
              </thead>
              <tbody>
                ${n.map(function(p,a){return d`<tr>
                    <td class="row-num">${s+a+1}</td>
                    ${p.map(function(h){return d`<td title="${h!=null?String(h):""}">${h!=null?String(h):""}</td>`})}
                  </tr>`})}
              </tbody>
            </table>
          `}
      </div>
      <div class="pagination">
        <button ?disabled=${this._page===0} @click=${function(){e._page--,e._rerunQuery()}}>←</button>
        <span>Page ${this._page+1}${i!=null?" of "+i:""}</span>
        <button ?disabled=${i!=null&&this._page>=i-1} @click=${function(){e._page++,e._rerunQuery()}}>→</button>
        <span>|</span>
        <select .value=${String(this._pageSize)} @change=${function(p){e._pageSize=parseInt(p.target.value,10),e._page=0,e._rerunQuery()}}>
          ${hn.map(function(p){return d`<option value=${p} ?selected=${p===e._pageSize}>${p} rows</option>`})}
        </select>
        <span class="duration">${this._result.durationMs!=null?this._result.durationMs+"ms":""}</span>
        ${typeof r=="number"?d`<span>${r} total rows</span>`:w}
      </div>
    `}_renderSummarizeTab(){if(this._loading)return d`<div class="loading-msg">Running SUMMARIZE...</div>`;if(!this._summarizeResult)return d`<div class="empty-state">Click "Summarize" to see column statistics</div>`;if(this._summarizeResult.error)return d`<div class="error-msg">${this._summarizeResult.error}</div>`;var e=this._summarizeResult.columns||[],t=this._summarizeResult.rows||[];return d`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              ${e.map(function(n){return d`<th>${n.name}<span class="col-type">${n.type}</span></th>`})}
            </tr>
          </thead>
          <tbody>
            ${t.map(function(n){return d`<tr>${n.map(function(r){return d`<td title="${r!=null?String(r):""}">${r!=null?String(r):""}</td>`})}</tr>`})}
          </tbody>
        </table>
      </div>
      <div class="status-bar">
        <span class="duration">${this._summarizeResult.durationMs!=null?this._summarizeResult.durationMs+"ms":""}</span>
      </div>
    `}_onOpen(){this._open=!0,this._fetchMetadata()}_close(){this._open=!1,this._historyOpen=!1}_onKeydown(e){if(this._open&&e.key==="Escape"){if(this._historyOpen){this._historyOpen=!1;return}this._close()}}_onExplorerClick(){this._historyOpen&&(this._historyOpen=!1)}_onDocsChanged(){this._open&&this._fetchMetadata()}_onRefresh(){if(!this._refreshing){this._refreshing=!0;var e=this;this._fetchMetadata(function(){e._refreshing=!1})}}_onSqlInput(e){this._sql=e.target.value,be(ue,this._sql)}_setSql(e){this._sql=e,be(ue,e)}_maxEditorHeight(){var e=this.renderRoot.querySelector(".main-panel");return e?Math.max(e.clientHeight-pn,ne):600}_startEditorDrag(e){e.preventDefault();var t=this,n=e.clientY,r=this._editorHeight,i=this._maxEditorHeight();this._dragging="editor";function s(a){t._editorHeight=j(r+(a.clientY-n),ne,i)}function p(){window.removeEventListener("pointermove",s),window.removeEventListener("pointerup",p),t._dragging=!1,t._saveLayout()}window.addEventListener("pointermove",s),window.addEventListener("pointerup",p)}_startSidebarDrag(e){e.preventDefault();var t=this,n=e.clientX,r=this._sidebarWidth;this._dragging="sidebar";function i(p){t._sidebarWidth=j(r+(p.clientX-n),he,pe)}function s(){window.removeEventListener("pointermove",i),window.removeEventListener("pointerup",s),t._dragging=!1,t._saveLayout()}window.addEventListener("pointermove",i),window.addEventListener("pointerup",s)}_onEditorSplitterKey(e){var t=16;e.key==="ArrowUp"?(e.preventDefault(),this._editorHeight=j(this._editorHeight-t,ne,this._maxEditorHeight()),this._saveLayout()):e.key==="ArrowDown"&&(e.preventDefault(),this._editorHeight=j(this._editorHeight+t,ne,this._maxEditorHeight()),this._saveLayout())}_onSidebarSplitterKey(e){var t=16;e.key==="ArrowLeft"?(e.preventDefault(),this._sidebarWidth=j(this._sidebarWidth-t,he,pe),this._saveLayout()):e.key==="ArrowRight"&&(e.preventDefault(),this._sidebarWidth=j(this._sidebarWidth+t,he,pe),this._saveLayout())}_saveLayout(){be(kt,{sidebarWidth:this._sidebarWidth,editorHeight:this._editorHeight})}_recordHistory(e){var t={sql:e,ts:Date.now()},n=this._history.filter(function(r){return r.sql!==e});n.unshift(t),n.length>$t&&(n.length=$t),this._history=n,be(Ne,n)}_applyHistoryEntry(e){this._setSql(e.sql),this._historyOpen=!1,this._page=0,this._runQuery()}_formatHistoryTime(e){if(!e)return"";var t=new Date(e),n=new Date,r=t.toDateString()===n.toDateString();return r?t.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"}):t.toLocaleDateString([],{month:"short",day:"numeric"})}_onEditorKeydown(e){if(e.key==="Tab"){e.preventDefault();var t=e.target,n=t.selectionStart,r=t.selectionEnd;this._setSql(this._sql.substring(0,n)+"  "+this._sql.substring(r)),this.updateComplete.then(function(){t.selectionStart=t.selectionEnd=n+2});return}e.key==="Enter"&&(e.metaKey||e.ctrlKey)&&(e.preventDefault(),this._runQuery())}_selectSource(e){this._setSql('SELECT * FROM "'+e+'"'),this._page=0,this._activeTab="results",this._runQuery()}_selectDataset(e){e&&e.sql?this._setSql(e.sql):e&&e.sqlError?this._setSql('-- Cannot resolve SQL for dataset "'+e.name+`":
-- `+e.sqlError):this._setSql('-- Dataset "'+(e&&e.name)+'" has no resolvable SQL.'),this._page=0,this._activeTab="results",e&&e.sql&&this._runQuery()}_toggleSourceInfo(e){this._expandedSource=this._expandedSource===e?null:e}_clearEditor(){this._setSql(""),this._result=null,this._summarizeResult=null,this._error="",this._page=0}_rerunQuery(){this._sql.trim()&&this._runQuery()}_runQuery(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="results",this._recordHistory(e),fetch(T()+"/__explorer/query",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e,limit:this._pageSize,offset:this._page*this._pageSize})}).then(function(n){return n.json()}).then(function(n){t._result=n,t._loading=!1}).catch(function(n){t._error=n.message||"Query failed",t._loading=!1})}}_runSummarize(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="summarize",fetch(T()+"/__explorer/summarize",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e})}).then(function(n){return n.json()}).then(function(n){t._summarizeResult=n,t._loading=!1}).catch(function(n){t._error=n.message||"Summarize failed",t._loading=!1})}}_exportCSV(){var e=this._sql.trim();if(!(!e||this._exporting)){var t=this;this._exporting=!0,fetch(T()+"/__explorer/export",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e})}).then(function(n){var r=n.headers.get("Content-Type")||"";return r.indexOf("text/csv")===-1?n.json().then(function(i){throw new Error(i.error||"Export failed")}):n.blob()}).then(function(n){var r=URL.createObjectURL(n),i=document.createElement("a");i.href=r,i.download="bino-explorer-export.csv",i.click(),URL.revokeObjectURL(r),t._exporting=!1}).catch(function(n){t._error=n.message||"Export failed",t._activeTab="results",t._exporting=!1})}}_fetchMetadata(e){var t=this;fetch(T()+"/__explorer/metadata").then(function(n){return n.json()}).then(function(n){t._metadata=n,e&&e()}).catch(function(n){console.error("explorer: fetch metadata failed",n),e&&e()})}};customElements.define("bino-data-explorer",Ie);var bn=3,He=class extends z{static properties={_open:{state:!0},_supported:{state:!0},_state:{state:!0},_findings:{state:!0},_elements:{state:!0},_selectedId:{state:!0},_detail:{state:!0},_showBoxes:{state:!0},_provisional:{state:!0},_busy:{state:!0},_error:{state:!0}};static styles=L`
    :host {
      position: fixed;
      top: var(--bino-toolbar-height);
      right: 0;
      bottom: 0;
      width: var(--bino-sidebar-width);
      max-width: 100vw;
      z-index: var(--bino-z-panel);
      background: var(--bino-surface);
      border-left: 1px solid var(--bino-border);
      box-shadow: var(--bino-shadow-dropdown);
      font-family: var(--bino-font-sans);
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
      display: none;
      flex-direction: column;
    }
    :host([open]) {
      display: flex;
    }
    .header {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-bottom: 1px solid var(--bino-border);
      font-weight: 600;
      color: var(--bino-text-muted);
      flex-shrink: 0;
    }
    .header .spacer {
      flex: 1;
    }
    .icon-btn {
      background: none;
      border: none;
      cursor: pointer;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-md);
      padding: 2px 4px;
      border-radius: var(--bino-radius);
      font-family: inherit;
    }
    .icon-btn:hover {
      background: var(--bino-surface-hover);
      color: var(--bino-text);
    }
    .icon-btn[aria-pressed='true'] {
      background: var(--bino-surface-active);
      color: var(--bino-active-text);
    }
    .body {
      overflow-y: auto;
      flex: 1;
    }
    .notice {
      padding: var(--bino-space-md);
      color: var(--bino-text-secondary);
      line-height: 1.5;
    }
    .notice.provisional {
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      margin: 0;
      padding: var(--bino-space-sm) var(--bino-space-md);
    }
    .notice code {
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      background: var(--bino-surface-inset);
      padding: 1px 4px;
      border-radius: 4px;
    }
    .section-title {
      padding: var(--bino-space-sm) var(--bino-space-md) var(--bino-space-xs);
      font-size: var(--bino-font-size-xs);
      text-transform: uppercase;
      letter-spacing: 0.04em;
      color: var(--bino-text-secondary);
      font-weight: 600;
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    .finding {
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-left: 3px solid var(--bino-warning);
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      cursor: pointer;
      line-height: 1.4;
    }
    .finding.error {
      border-left-color: var(--bino-error);
      background: var(--bino-error-bg);
      border-bottom-color: var(--bino-error-border);
    }
    .finding-label {
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    .finding-hint {
      display: block;
      margin-top: 2px;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
    .row {
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-bottom: 1px solid var(--bino-border);
      cursor: pointer;
    }
    .row:hover {
      background: var(--bino-surface-hover);
    }
    .row.selected {
      background: var(--bino-surface-active);
    }
    .row-label {
      font-weight: 600;
      display: flex;
      align-items: baseline;
      gap: var(--bino-space-xs);
    }
    .row-tag {
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      font-weight: 400;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs);
      margin-top: var(--bino-space-xs);
    }
    .chip {
      padding: 1px 6px;
      border-radius: var(--bino-radius-pill);
      font-size: var(--bino-font-size-xs);
      background: var(--bino-surface-inset);
      color: var(--bino-text-secondary);
      white-space: nowrap;
    }
    .chip.scale {
      background: var(--bino-celeste-100);
      color: var(--bino-celeste-800);
    }
    .chip.warn {
      background: var(--bino-warning-bg);
      color: var(--bino-warning-text);
    }
    .chip.bad {
      background: var(--bino-error-bg);
      color: var(--bino-bad);
    }
    .detail {
      padding: var(--bino-space-sm) var(--bino-space-md);
      background: var(--bino-surface-subtle);
      border-bottom: 1px solid var(--bino-border);
      line-height: 1.5;
    }
    .detail dl {
      margin: 0;
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 2px var(--bino-space-sm);
    }
    .detail dt {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
    .detail dd {
      margin: 0;
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      overflow-wrap: anywhere;
    }
    .detail-actions {
      margin-top: var(--bino-space-sm);
      display: flex;
      gap: var(--bino-space-xs);
    }
    .text-btn {
      background: var(--bino-surface);
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius-pill);
      padding: 2px 10px;
      font-size: var(--bino-font-size-xs);
      font-family: inherit;
      color: var(--bino-text-secondary);
      cursor: pointer;
    }
    .text-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
  `;constructor(){super(),this._open=!1,this._supported=!0,this._state=null,this._findings=[],this._elements=new Map,this._selectedId="",this._detail=null,this._showBoxes=!1,this._provisional=!1,this._retries=0,this._busy=!1,this._error="",this._captureTimer=null,this._overlays=[],this._boundOnOpen=this._onOpen.bind(this),this._boundOnContentUpdated=this._onContentUpdated.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-inspector",this._boundOnOpen),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-inspector",this._boundOnOpen),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated),document.removeEventListener("keydown",this._boundOnKeydown),this._captureTimer&&clearTimeout(this._captureTimer),this._clearOverlays()}updated(e){e.has("_open")&&(this._open?this.setAttribute("open",""):this.removeAttribute("open"))}render(){return d`
      <div class="header">
        <span>Inspector</span>
        <span class="spacer"></span>
        ${this._supported?d`
              <button class="icon-btn" title="Outline every component"
                aria-pressed=${this._showBoxes?"true":"false"} @click=${this._onToggleBoxes}>□</button>
              <button class="icon-btn" title="Copy the snapshot as JSON" @click=${this._onCopy}>⎘</button>
              <button class="icon-btn" title="Re-capture" @click=${this._onRefresh}>↻</button>
            `:w}
        <button class="icon-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <div class="body">${this._renderBody()}</div>
    `}_renderBody(){if(!this._supported)return d`
        <p class="notice">
          This template engine does not expose layout state. The inspector needs
          <code>bn-template-engine v1.0.0-next.24</code> or newer - pin it with
          <code>engine-version</code> in <code>bino.toml</code>.
        </p>
      `;if(this._error)return d`<p class="notice">${this._error}</p>`;if(this._busy&&!this._state)return d`<p class="notice">Capturing…</p>`;if(!this._state)return d`<p class="notice">No snapshot yet.</p>`;var e=this._state.components||[];return d`
      ${this._provisional?d`
            <p class="notice provisional">
              The report is still rendering, so these boxes are provisional and
              no checks were run. Re-capturing…
            </p>
          `:w}
      ${this._findings.length>0?d`
            <div class="section-title">${this._findings.length} finding${this._findings.length===1?"":"s"}</div>
            <ul>${this._findings.map(t=>this._renderFinding(t))}</ul>
          `:w}
      <div class="section-title">${e.length} component${e.length===1?"":"s"}</div>
      <ul>${e.map(t=>this._renderComponent(t))}</ul>
    `}_renderFinding(e){var t=e.name?e.kind?e.kind+" "+e.name:e.name:e.componentId;return d`
      <li class="finding ${e.severity==="error"?"error":""}"
        @click=${()=>this._select(e.componentId)}>
        <span class="finding-label">${t}</span> - ${e.message}
        ${e.hint?d`<span class="finding-hint">${e.hint}</span>`:w}
      </li>
    `}_renderComponent(e){var t=this._state.sources&&this._state.sources[e.id]||{},n=t.name||t.ref||e.id,r=e.id===this._selectedId;return d`
      <li>
        <div class="row ${r?"selected":""}"
          @click=${i=>this._onRowClick(i,e.id)}
          @mouseenter=${()=>this._outline(e.id)}
          @mouseleave=${()=>this._clearHoverOutline()}>
          <div class="row-label">
            <span>${t.kind?t.kind+" "+n:n}</span>
            <span class="row-tag">${e.tag}</span>
          </div>
          <div class="chips">
            <span class="chip">${this._formatBox(e)}</span>
            ${this._countChips(e)}
            ${this._scaleChip(e)}
            ${this._diagChips(e)}
          </div>
        </div>
        ${r?this._renderDetail(e):w}
      </li>
    `}_renderDetail(e){var t=[],n=e.em||{};t.push(["font",this._round(n.fontSizePx)+"px"]),n.appliedScaleFactor!=null&&n.appliedScaleFactor!==1&&t.push(["auto-fit",Math.round(n.appliedScaleFactor*100)+"%"]);var r=e.scaling||{};r.unitsPerEm!=null&&t.push(["units/em",this._round(r.unitsPerEm)+" ("+(r.unitMode||"auto")+")"]),r.percentagePointsPerEm!=null&&t.push(["pp/em",this._round(r.percentagePointsPerEm)+" ("+(r.percentageMode||"auto")+")"]),(e.regions||[]).forEach(function(s){t.push([s.id,Math.round(s.rect.component.width)+"\xD7"+Math.round(s.rect.component.height)])});var i=e.metadata||{};return i.scenarios&&i.scenarios.length>0&&t.push(["scenarios",i.scenarios.join(", ")]),i.variances&&i.variances.length>0&&t.push(["variances",i.variances.join(", ")]),(e.diagnostics||[]).forEach(function(s){t.push([s.id||s.type||"diagnostic",s.message||""])}),d`
      <div class="detail">
        <dl>${t.map(s=>d`<dt>${s[0]}</dt><dd>${s[1]}</dd>`)}</dl>
        ${this._detail?this._renderElementDetail():w}
        <div class="detail-actions">
          ${this._detail?w:d`<button class="text-btn" @click=${()=>this._loadDetail(e.id)}>Element detail</button>`}
          <button class="text-btn" @click=${()=>this._revealSource(e.id)}>Reveal source</button>
        </div>
      </div>
    `}_renderElementDetail(){var e={};(this._detail.elements||[]).forEach(function(r){e[r.kind]=(e[r.kind]||0)+1});var t=Object.keys(e).sort(),n=this._detail.table&&this._detail.table.columns||[];return d`
      <dl>
        ${t.map(r=>d`<dt>${r}</dt><dd>${e[r]}</dd>`)}
        ${n.map(r=>d`<dt>col ${r.index}</dt><dd>${r.key}${r.bucket?" \xB7 "+r.bucket:""}</dd>`)}
      </dl>
    `}_formatBox(e){var t=e.rect&&e.rect.component||{width:0,height:0};return Math.round(t.width)+"\xD7"+Math.round(t.height)}_countChips(e){var t=e.metadata||{},n=[["bars",t.barCount],["points",t.pointCount],["rows",t.rowCount],["nodes",t.nodeCount]];return n.filter(function(r){return r[1]!=null}).map(function(r){return d`<span class="chip ${r[1]===0?"warn":""}">${r[1]} ${r[0]}</span>`})}_scaleChip(e){var t=e.scaling||{};return t.unitsPerEm==null?w:d`<span class="chip scale">${t.unitMode||"auto"} ${this._round(t.unitsPerEm)}/em</span>`}_diagChips(e){return(e.diagnostics||[]).map(function(t){return d`<span class="chip ${t.type==="error"?"bad":"warn"}">${t.id||t.type}</span>`})}_round(e){return e==null?"":Math.round(e*100)/100}_onOpen(){this._open=!0,this._retries=0,this._capture()}_onClose(){this._open=!1,this._showBoxes=!1,this._clearOverlays()}_onKeydown(e){e.key==="Escape"&&this._open&&this._onClose()}_onContentUpdated(){if(this._open){this._captureTimer&&clearTimeout(this._captureTimer);var e=this;this._captureTimer=setTimeout(function(){e._retries=0,e._capture()},250)}}_onRefresh(){this._retries=0,this._capture()}_scheduleRetry(){if(!(this._retries>=bn)){this._retries++;var e=this;this._captureTimer&&clearTimeout(this._captureTimer),this._captureTimer=setTimeout(function(){e._capture()},1e3)}}async _capture(){if(this._supported=de(),!this._supported){this._state=null;return}this._busy=!0,this._error="";try{var e=await _t({settleTimeoutMs:5e3});if(!e){this._supported=!1;return}var t=this._selectedId;this._elements=e.elements,this._detail=null,this._state=Object.assign({},e.state,{sources:e.sources}),this._selectedId=e.elements.has(t)?t:"",this._provisional=!e.settled,this._provisional?(this._findings=[],this._scheduleRetry()):this._findings=await this._analyze(e),this._showBoxes&&this._drawAllOutlines()}catch(n){console.error("bino inspector: capture failed",n),this._error="Capture failed: "+(n&&n.message?n.message:n)}finally{this._busy=!1}}async _analyze(e){try{var t=await fetch(T()+"/__bino/layout-state",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({state:e.state,sources:e.sources})});if(!t.ok)return console.warn("bino inspector: analysis failed",t.status),[];var n=await t.json();return n.findings||[]}catch(r){return console.warn("bino inspector: analysis request failed",r),[]}}async _loadDetail(e){var t=this._elements.get(e);try{this._detail=await yt(t)||{elements:[]}}catch(n){console.warn("bino inspector: element detail failed",n),this._detail={elements:[]}}}_onRowClick(e,t){if(e.metaKey||e.ctrlKey){this._revealSource(t);return}this._select(t)}_select(e){if(this._selectedId===e){this._selectedId="",this._detail=null;return}this._selectedId=e,this._detail=null;var t=this._elements.get(e);t&&(t.scrollIntoView({behavior:"smooth",block:"center"}),this._outline(e,!0))}_revealSource(e){var t=this._elements.get(e),n=t&&t.closest?t.closest("[data-bino-kind]"):null;if(n){var r={type:"bino:revealSource",kind:n.getAttribute("data-bino-kind"),name:n.getAttribute("data-bino-name")||"",ref:n.getAttribute("data-bino-ref")||""},i=parseInt(n.getAttribute("data-bino-source-line")||"",10);i>0&&(r.sourceFile=n.getAttribute("data-bino-source-file")||"",r.line=i),window.parent&&window.parent!==window&&window.parent.postMessage(r,"*")}}_outline(e,t){var n=this._elements.get(e);n&&(this._showBoxes||this._clearOverlays(),this._overlays.push(this._makeOutline(n,t?"selected":"hover")))}_clearHoverOutline(){this._showBoxes||this._clearOverlays()}_onToggleBoxes(){this._showBoxes=!this._showBoxes,this._clearOverlays(),this._showBoxes&&this._drawAllOutlines()}_drawAllOutlines(){var e=this;this._elements.forEach(function(t){e._overlays.push(e._makeOutline(t,"all"))})}_makeOutline(e,t){var n=e.getBoundingClientRect(),r=document.createElement("div");return r.className="bn-inspect-outline",r.style.cssText="position:absolute;pointer-events:none;left:"+(n.left+window.scrollX)+"px;top:"+(n.top+window.scrollY)+"px;width:"+n.width+"px;height:"+n.height+"px;",t==="selected"&&r.classList.add("selected"),document.body.appendChild(r),r}_clearOverlays(){this._overlays.forEach(function(e){e.parentNode&&e.parentNode.removeChild(e)}),this._overlays=[]}async _onCopy(){if(this._state){var e=JSON.stringify({state:this._state,findings:this._findings},null,2);try{await navigator.clipboard.writeText(e)}catch(t){console.warn("bino inspector: clipboard write failed",t)}}}};customElements.define("bino-inspector",He);if(!(!window.EventSource||window.__bnPreviewRuntime)){let o=function(){Ue&&Be&&e("initial")},e=function(n){var r=++Ke;fetch(qe+"/__preview/context?path="+encodeURIComponent(P)).then(function(i){return i.ok?i.text():(console.warn("bn preview: context fetch failed",i.status,n),null)}).then(function(i){if(!(!i||r!==Ke)){var s=Qe(i,Et);if(s){je=!0;try{document.dispatchEvent(new CustomEvent("bn-preview:content-updated",{detail:{path:P}}))}catch(p){console.debug("bn preview: custom event skipped",p)}}else console.warn("bn preview: swapContext returned false; DOM not updated")}}).catch(function(i){console.error("bn preview: context fetch errored",i)})},t=function(){if(P==="/"){document.querySelectorAll(".bn-page-info").forEach(function(h){h.remove()});var n=document.querySelector("bn-context");if(n){var r=n.getAttribute("data-page-meta");if(r){var i;try{i=JSON.parse(r)}catch{return}if(Array.isArray(i)){var s={};i.forEach(function(h){s[h.name]=h});var p=n.shadowRoot||n,a=p.querySelectorAll("bn-layout-page[data-bino-page]");a.length===0&&(a=document.querySelectorAll("bn-layout-page[data-bino-page]")),a.forEach(function(h){var v=h.getAttribute("data-bino-page");if(v){var f=v.split("#")[0],b=s[f]||s[v];if(b){var y=document.createElement("div");y.className="bn-page-info";var x=document.createElement("span");if(x.className="bn-page-info-name",x.textContent=v,y.appendChild(x),b.constraints&&b.constraints.length>0){var S=document.createElement("span");S.className="bn-page-info-label",S.textContent="constraints:",y.appendChild(S),b.constraints.forEach(function($){var u=document.createElement("span");u.className="bn-page-info-pill constraint",u.textContent=$,y.appendChild(u)})}if(b.artifacts&&b.artifacts.length>0){var C=document.createElement("span");C.className="bn-page-info-label",C.textContent="used in:",y.appendChild(C),b.artifacts.forEach(function($){var u=document.createElement("span");u.className="bn-page-info-pill artefact",u.textContent=$,y.appendChild(u)})}h.parentNode.insertBefore(y,h)}}})}}}}};window.__bnPreviewRuntime=!0,console.info("bn preview runtime v14 (plugin reload)"),Et=new DOMParser,qe=T(),P=ie(),I=new EventSource(qe+"/__preview/events"),Ue=!1,Be=!1,We().then(function(){Be=!0,o()}),I.addEventListener("ready",function(){Ue=!0,document.dispatchEvent(new CustomEvent("bn-preview:refresh-done")),o()}),I.addEventListener("refreshing",function(n){try{var r=JSON.parse(n.data||"{}");document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:r}))}catch{document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:{}}))}}),I.addEventListener("refresh-done",function(n){var r={};try{r=JSON.parse(n.data||"{}")||{}}catch{r={}}var i=Array.isArray(r.paths)?r.paths:[],s=!1;if(i.length>0){for(var p=0;p<i.length;p++)if(W(i[p])===P){s=!0;break}s||(console.warn("bn preview: refresh did not include this view",P,"broadcast paths:",i),document.dispatchEvent(new CustomEvent("bn-preview:no-payload",{detail:{path:P,broadcastPaths:i}})))}s&&!je&&e("initial-retry"),document.dispatchEvent(new CustomEvent("bn-preview:refresh-done",{detail:r}))}),Ke=0,je=!1,I.addEventListener("path-changed",function(n){var r={};try{r=JSON.parse(n.data||"{}")||{}}catch{return}!r.path||W(r.path)!==P||e("path-changed")}),I.addEventListener("refresh-error",function(n){var r={};try{r=JSON.parse(n.data||"{}")}catch{}r&&r.path&&W(r.path)!==P||(console.error("bn preview: refresh failed",r&&r.message),document.dispatchEvent(new CustomEvent("bn-preview:refresh-error",{detail:r})))}),I.addEventListener("plugins-reloaded",function(){location.reload()}),window.addEventListener("beforeunload",function(){I.close()}),document.addEventListener("click",function(n){if(!(!n.metaKey&&!n.ctrlKey)){var r=n.target.closest("[data-bino-kind], [data-bino-file]");if(r){var i;if(r.hasAttribute("data-bino-kind")){i={type:"bino:revealSource",kind:r.getAttribute("data-bino-kind"),name:r.getAttribute("data-bino-name")||"",ref:r.getAttribute("data-bino-ref")||""};var s=parseInt(r.getAttribute("data-bino-source-line")||"",10);s>0&&(i.sourceFile=r.getAttribute("data-bino-source-file")||"",i.line=s)}else i={type:"bino:revealSource",file:r.getAttribute("data-bino-file")};window.parent&&window.parent!==window&&window.parent.postMessage(i,"*"),n.preventDefault(),n.stopPropagation()}}}),Xe(window),document.addEventListener("bn-preview:content-updated",function(){t()}),document.readyState==="loading"?document.addEventListener("DOMContentLoaded",t):t()}var Et,qe,P,I,Ue,Be,Ke,je;
/*! Bundled license information:

@lit/reactive-element/css-tag.js:
  (**
   * @license
   * Copyright 2019 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/reactive-element.js:
lit-html/lit-html.js:
lit-element/lit-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/is-server.js:
  (**
   * @license
   * Copyright 2022 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)
*/
