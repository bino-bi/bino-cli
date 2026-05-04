var Ke=Object.defineProperty;var Ee=(n,e)=>()=>(n&&(e=n(n=0)),e);var Je=(n,e)=>{for(var t in e)Ke(n,t,{get:e[t],enumerable:!0})};var je,Ve=Ee(()=>{je=(function(){"use strict";let n=()=>{},e={morphStyle:"outerHTML",callbacks:{beforeNodeAdded:n,afterNodeAdded:n,beforeNodeMorphed:n,afterNodeMorphed:n,beforeNodeRemoved:n,afterNodeRemoved:n,beforeAttributeUpdated:n},head:{style:"merge",shouldPreserve:p=>p.getAttribute("im-preserve")==="true",shouldReAppend:p=>p.getAttribute("im-re-append")==="true",shouldRemove:n,afterHeadMorphed:n},restoreFocus:!0};function t(p,_,v={}){p=x(p);let S=E(_),w=A(p,S,v),b=i(w,()=>$(w,p,S,l=>l.morphStyle==="innerHTML"?(o(l,p,S),Array.from(p.childNodes)):r(l,p,S)));return w.pantry.remove(),b}function r(p,_,v){let S=E(_);return o(p,S,v,_,_.nextSibling),Array.from(S.childNodes)}function i(p,_){if(!p.config.restoreFocus)return _();let v=document.activeElement;if(!(v instanceof HTMLInputElement||v instanceof HTMLTextAreaElement))return _();let{id:S,selectionStart:w,selectionEnd:b}=v,l=_();return S&&S!==document.activeElement?.getAttribute("id")&&(v=p.target.querySelector(`[id="${S}"]`),v?.focus()),v&&!v.selectionEnd&&b&&v.setSelectionRange(w,b),l}let o=(function(){function p(s,a,c,u=null,f=null){a instanceof HTMLTemplateElement&&c instanceof HTMLTemplateElement&&(a=a.content,c=c.content),u||=a.firstChild;for(let g of c.childNodes){if(u&&u!=f){let C=v(s,g,u,f);if(C){C!==u&&w(s,u,C),d(C,g,s),u=C.nextSibling;continue}}if(g instanceof Element){let C=g.getAttribute("id");if(s.persistentIds.has(C)){let L=b(a,C,u,s);d(L,g,s),u=L.nextSibling;continue}}let y=_(a,g,u,s);y&&(u=y.nextSibling)}for(;u&&u!=f;){let g=u;u=u.nextSibling,S(s,g)}}function _(s,a,c,u){if(u.callbacks.beforeNodeAdded(a)===!1)return null;if(u.idMap.has(a)){let f=document.createElement(a.tagName);return s.insertBefore(f,c),d(f,a,u),u.callbacks.afterNodeAdded(f),f}else{let f=document.importNode(a,!0);return s.insertBefore(f,c),u.callbacks.afterNodeAdded(f),f}}let v=(function(){function s(u,f,g,y){let C=null,L=f.nextSibling,Se=0,k=g;for(;k&&k!=y;){if(c(k,f)){if(a(u,k,f))return k;C===null&&(u.idMap.has(k)||(C=k))}if(C===null&&L&&c(k,L)&&(Se++,L=L.nextSibling,Se>=2&&(C=void 0)),u.activeElementAndParents.includes(k))break;k=k.nextSibling}return C||null}function a(u,f,g){let y=u.idMap.get(f),C=u.idMap.get(g);if(!C||!y)return!1;for(let L of y)if(C.has(L))return!0;return!1}function c(u,f){let g=u,y=f;return g.nodeType===y.nodeType&&g.tagName===y.tagName&&(!g.getAttribute?.("id")||g.getAttribute?.("id")===y.getAttribute?.("id"))}return s})();function S(s,a){if(s.idMap.has(a))h(s.pantry,a,null);else{if(s.callbacks.beforeNodeRemoved(a)===!1)return;a.parentNode?.removeChild(a),s.callbacks.afterNodeRemoved(a)}}function w(s,a,c){let u=a;for(;u&&u!==c;){let f=u;u=u.nextSibling,S(s,f)}return u}function b(s,a,c,u){let f=u.target.getAttribute?.("id")===a&&u.target||u.target.querySelector(`[id="${a}"]`)||u.pantry.querySelector(`[id="${a}"]`);return l(f,u),h(s,f,c),f}function l(s,a){let c=s.getAttribute("id");for(;s=s.parentNode;){let u=a.idMap.get(s);u&&(u.delete(c),u.size||a.idMap.delete(s))}}function h(s,a,c){if(s.moveBefore)try{s.moveBefore(a,c)}catch{s.insertBefore(a,c)}else s.insertBefore(a,c)}return p})(),d=(function(){function p(l,h,s){return s.ignoreActive&&l===document.activeElement?null:(s.callbacks.beforeNodeMorphed(l,h)===!1||(l instanceof HTMLHeadElement&&s.head.ignore||(l instanceof HTMLHeadElement&&s.head.style!=="morph"?m(l,h,s):(_(l,h,s),b(l,s)||o(s,l,h))),s.callbacks.afterNodeMorphed(l,h)),l)}function _(l,h,s){let a=h.nodeType;if(a===1){let c=l,u=h,f=c.attributes,g=u.attributes;for(let y of g)w(y.name,c,"update",s)||c.getAttribute(y.name)!==y.value&&c.setAttribute(y.name,y.value);for(let y=f.length-1;0<=y;y--){let C=f[y];if(C&&!u.hasAttribute(C.name)){if(w(C.name,c,"remove",s))continue;c.removeAttribute(C.name)}}b(c,s)||v(c,u,s)}(a===8||a===3)&&l.nodeValue!==h.nodeValue&&(l.nodeValue=h.nodeValue)}function v(l,h,s){if(l instanceof HTMLInputElement&&h instanceof HTMLInputElement&&h.type!=="file"){let a=h.value,c=l.value;S(l,h,"checked",s),S(l,h,"disabled",s),h.hasAttribute("value")?c!==a&&(w("value",l,"update",s)||(l.setAttribute("value",a),l.value=a)):w("value",l,"remove",s)||(l.value="",l.removeAttribute("value"))}else if(l instanceof HTMLOptionElement&&h instanceof HTMLOptionElement)S(l,h,"selected",s);else if(l instanceof HTMLTextAreaElement&&h instanceof HTMLTextAreaElement){let a=h.value,c=l.value;if(w("value",l,"update",s))return;a!==c&&(l.value=a),l.firstChild&&l.firstChild.nodeValue!==a&&(l.firstChild.nodeValue=a)}}function S(l,h,s,a){let c=h[s],u=l[s];if(c!==u){let f=w(s,l,"update",a);f||(l[s]=h[s]),c?f||l.setAttribute(s,""):w(s,l,"remove",a)||l.removeAttribute(s)}}function w(l,h,s,a){return l==="value"&&a.ignoreActiveValue&&h===document.activeElement?!0:a.callbacks.beforeAttributeUpdated(l,h,s)===!1}function b(l,h){return!!h.ignoreActiveValue&&l===document.activeElement&&l!==document.body}return p})();function $(p,_,v,S){if(p.head.block){let w=_.querySelector("head"),b=v.querySelector("head");if(w&&b){let l=m(w,b,p);return Promise.all(l).then(()=>{let h=Object.assign(p,{head:{block:!1,ignore:!0}});return S(h)})}}return S(p)}function m(p,_,v){let S=[],w=[],b=[],l=[],h=new Map;for(let a of _.children)h.set(a.outerHTML,a);for(let a of p.children){let c=h.has(a.outerHTML),u=v.head.shouldReAppend(a),f=v.head.shouldPreserve(a);c||f?u?w.push(a):(h.delete(a.outerHTML),b.push(a)):v.head.style==="append"?u&&(w.push(a),l.push(a)):v.head.shouldRemove(a)!==!1&&w.push(a)}l.push(...h.values());let s=[];for(let a of l){let c=document.createRange().createContextualFragment(a.outerHTML).firstChild;if(v.callbacks.beforeNodeAdded(c)!==!1){if("href"in c&&c.href||"src"in c&&c.src){let u,f=new Promise(function(g){u=g});c.addEventListener("load",function(){u()}),s.push(f)}p.appendChild(c),v.callbacks.afterNodeAdded(c),S.push(c)}}for(let a of w)v.callbacks.beforeNodeRemoved(a)!==!1&&(p.removeChild(a),v.callbacks.afterNodeRemoved(a));return v.head.afterHeadMorphed(p,{added:S,kept:b,removed:w}),s}let A=(function(){function p(s,a,c){let{persistentIds:u,idMap:f}=l(s,a),g=_(c),y=g.morphStyle||"outerHTML";if(!["innerHTML","outerHTML"].includes(y))throw`Do not understand how to morph style ${y}`;return{target:s,newContent:a,config:g,morphStyle:y,ignoreActive:g.ignoreActive,ignoreActiveValue:g.ignoreActiveValue,restoreFocus:g.restoreFocus,idMap:f,persistentIds:u,pantry:v(),activeElementAndParents:S(s),callbacks:g.callbacks,head:g.head}}function _(s){let a=Object.assign({},e);return Object.assign(a,s),a.callbacks=Object.assign({},e.callbacks,s.callbacks),a.head=Object.assign({},e.head,s.head),a}function v(){let s=document.createElement("div");return s.hidden=!0,document.body.insertAdjacentElement("afterend",s),s}function S(s){let a=[],c=document.activeElement;if(c?.tagName!=="BODY"&&s.contains(c))for(;c&&(a.push(c),c!==s);)c=c.parentElement;return a}function w(s){let a=Array.from(s.querySelectorAll("[id]"));return s.getAttribute?.("id")&&a.push(s),a}function b(s,a,c,u){for(let f of u){let g=f.getAttribute("id");if(a.has(g)){let y=f;for(;y;){let C=s.get(y);if(C==null&&(C=new Set,s.set(y,C)),C.add(g),y===c)break;y=y.parentElement}}}}function l(s,a){let c=w(s),u=w(a),f=h(c,u),g=new Map;b(g,f,s,c);let y=a.__idiomorphRoot||a;return b(g,f,y,u),{persistentIds:f,idMap:g}}function h(s,a){let c=new Set,u=new Map;for(let{id:g,tagName:y}of s)u.has(g)?c.add(g):u.set(g,y);let f=new Set;for(let{id:g,tagName:y}of a)f.has(g)?c.add(g):u.get(g)===y&&f.add(g);for(let g of c)f.delete(g);return f}return p})(),{normalizeElement:x,normalizeParent:E}=(function(){let p=new WeakSet;function _(b){return b instanceof Document?b.documentElement:b}function v(b){if(b==null)return document.createElement("div");if(typeof b=="string")return v(w(b));if(p.has(b))return b;if(b instanceof Node){if(b.parentNode)return new S(b);{let l=document.createElement("div");return l.append(b),l}}else{let l=document.createElement("div");for(let h of[...b])l.append(h);return l}}class S{constructor(l){this.originalNode=l,this.realParentNode=l.parentNode,this.previousSibling=l.previousSibling,this.nextSibling=l.nextSibling}get childNodes(){let l=[],h=this.previousSibling?this.previousSibling.nextSibling:this.realParentNode.firstChild;for(;h&&h!=this.nextSibling;)l.push(h),h=h.nextSibling;return l}querySelectorAll(l){return this.childNodes.reduce((h,s)=>{if(s instanceof Element){s.matches(l)&&h.push(s);let a=s.querySelectorAll(l);for(let c=0;c<a.length;c++)h.push(a[c])}return h},[])}insertBefore(l,h){return this.realParentNode.insertBefore(l,h)}moveBefore(l,h){return this.realParentNode.moveBefore(l,h)}get __idiomorphRoot(){return this.originalNode}}function w(b){let l=new DOMParser,h=b.replace(/<svg(\s[^>]*>|>)([\s\S]*?)<\/svg>/gim,"");if(h.match(/<\/html>/)||h.match(/<\/head>/)||h.match(/<\/body>/)){let s=l.parseFromString(b,"text/html");if(h.match(/<\/html>/))return p.add(s),s;{let a=s.firstChild;return a&&p.add(a),a}}else{let a=l.parseFromString("<body><template>"+b+"</template></body>","text/html").body.querySelector("template").content;return p.add(a),a}}return{normalizeElement:_,normalizeParent:v}})();return{morph:t,defaults:e}})()});var Fe={};Je(Fe,{decodeBase64:()=>re,escapeHtml:()=>lt,normalizePath:()=>dt,swapContext:()=>ge,waitForEngine:()=>ut});function re(n){if(!n)return"";try{return atob(n)}catch(e){return console.error("bino: decode failed",e),""}}function lt(n){var e=document.createElement("div");return e.textContent=n,e.innerHTML}function dt(n){return n?n.charAt(0)==="/"?n:"/"+n:"/"}function ut(){return customElements.get("bn-context")?Promise.resolve():customElements.whenDefined("bn-context")}function ge(n,e){if(!n)return console.debug("bino: swapContext skipped \u2014 empty html"),!1;e||(e=new DOMParser);var t=e.parseFromString(n,"text/html"),r=t.querySelector("bn-context"),i=document.querySelector("bn-context");return r?i?(ct(i,r),je.morph(i,r.innerHTML,{morphStyle:"innerHTML",callbacks:{beforeAttributeUpdated:function(o,d,$){if(o==="class"&&d.tagName&&d.tagName.includes("-"))return!1}}}),!0):(console.debug("bino: swapContext skipped \u2014 live DOM has no <bn-context>"),!1):(console.debug("bino: swapContext skipped \u2014 incoming HTML has no <bn-context>"),!1)}function ct(n,e){for(var t=0;t<e.attributes.length;t++){var r=e.attributes[t];r.name!=="class"&&n.getAttribute(r.name)!==r.value&&n.setAttribute(r.name,r.value)}for(var i=n.attributes.length-1;i>=0;i--){var o=n.attributes[i].name;o!=="class"&&(e.hasAttribute(o)||n.removeAttribute(o))}}var ve=Ee(()=>{Ve()});var X=globalThis,Z=X.ShadowRoot&&(X.ShadyCSS===void 0||X.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,ie=Symbol(),we=new WeakMap,j=class{constructor(e,t,r){if(this._$cssResult$=!0,r!==ie)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(Z&&e===void 0){let r=t!==void 0&&t.length===1;r&&(e=we.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),r&&we.set(t,e))}return e}toString(){return this.cssText}},xe=n=>new j(typeof n=="string"?n:n+"",void 0,ie),V=(n,...e)=>{let t=n.length===1?n[0]:e.reduce((r,i,o)=>r+(d=>{if(d._$cssResult$===!0)return d.cssText;if(typeof d=="number")return d;throw Error("Value passed to 'css' function must be a 'css' function result: "+d+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+n[o+1],n[0]);return new j(t,n,ie)},Ce=(n,e)=>{if(Z)n.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let r=document.createElement("style"),i=X.litNonce;i!==void 0&&r.setAttribute("nonce",i),r.textContent=t.cssText,n.appendChild(r)}},ne=Z?n=>n:n=>n instanceof CSSStyleSheet?(e=>{let t="";for(let r of e.cssRules)t+=r.cssText;return xe(t)})(n):n;var{is:Ge,defineProperty:Ye,getOwnPropertyDescriptor:Qe,getOwnPropertyNames:Xe,getOwnPropertySymbols:Ze,getPrototypeOf:et}=Object,ee=globalThis,Pe=ee.trustedTypes,tt=Pe?Pe.emptyScript:"",rt=ee.reactiveElementPolyfillSupport,F=(n,e)=>n,se={toAttribute(n,e){switch(e){case Boolean:n=n?tt:null;break;case Object:case Array:n=n==null?n:JSON.stringify(n)}return n},fromAttribute(n,e){let t=n;switch(e){case Boolean:t=n!==null;break;case Number:t=n===null?null:Number(n);break;case Object:case Array:try{t=JSON.parse(n)}catch{t=null}}return t}},ke=(n,e)=>!Ge(n,e),Me={attribute:!0,type:String,converter:se,reflect:!1,useDefault:!1,hasChanged:ke};Symbol.metadata??=Symbol("metadata"),ee.litPropertyMetadata??=new WeakMap;var H=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??=[]).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=Me){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let r=Symbol(),i=this.getPropertyDescriptor(e,r,t);i!==void 0&&Ye(this.prototype,e,i)}}static getPropertyDescriptor(e,t,r){let{get:i,set:o}=Qe(this.prototype,e)??{get(){return this[t]},set(d){this[t]=d}};return{get:i,set(d){let $=i?.call(this);o?.call(this,d),this.requestUpdate(e,$,r)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??Me}static _$Ei(){if(this.hasOwnProperty(F("elementProperties")))return;let e=et(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(F("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(F("properties"))){let t=this.properties,r=[...Xe(t),...Ze(t)];for(let i of r)this.createProperty(i,t[i])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[r,i]of t)this.elementProperties.set(r,i)}this._$Eh=new Map;for(let[t,r]of this.elementProperties){let i=this._$Eu(t,r);i!==void 0&&this._$Eh.set(i,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let r=new Set(e.flat(1/0).reverse());for(let i of r)t.unshift(ne(i))}else e!==void 0&&t.push(ne(e));return t}static _$Eu(e,t){let r=t.attribute;return r===!1?void 0:typeof r=="string"?r:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??=new Set).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let r of t.keys())this.hasOwnProperty(r)&&(e.set(r,this[r]),delete this[r]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return Ce(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,r){this._$AK(e,r)}_$ET(e,t){let r=this.constructor.elementProperties.get(e),i=this.constructor._$Eu(e,r);if(i!==void 0&&r.reflect===!0){let o=(r.converter?.toAttribute!==void 0?r.converter:se).toAttribute(t,r.type);this._$Em=e,o==null?this.removeAttribute(i):this.setAttribute(i,o),this._$Em=null}}_$AK(e,t){let r=this.constructor,i=r._$Eh.get(e);if(i!==void 0&&this._$Em!==i){let o=r.getPropertyOptions(i),d=typeof o.converter=="function"?{fromAttribute:o.converter}:o.converter?.fromAttribute!==void 0?o.converter:se;this._$Em=i;let $=d.fromAttribute(t,o.type);this[i]=$??this._$Ej?.get(i)??$,this._$Em=null}}requestUpdate(e,t,r,i=!1,o){if(e!==void 0){let d=this.constructor;if(i===!1&&(o=this[e]),r??=d.getPropertyOptions(e),!((r.hasChanged??ke)(o,t)||r.useDefault&&r.reflect&&o===this._$Ej?.get(e)&&!this.hasAttribute(d._$Eu(e,r))))return;this.C(e,t,r)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:r,reflect:i,wrapped:o},d){r&&!(this._$Ej??=new Map).has(e)&&(this._$Ej.set(e,d??t??this[e]),o!==!0||d!==void 0)||(this._$AL.has(e)||(this.hasUpdated||r||(t=void 0),this._$AL.set(e,t)),i===!0&&this._$Em!==e&&(this._$Eq??=new Set).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[i,o]of this._$Ep)this[i]=o;this._$Ep=void 0}let r=this.constructor.elementProperties;if(r.size>0)for(let[i,o]of r){let{wrapped:d}=o,$=this[i];d!==!0||this._$AL.has(i)||$===void 0||this.C(i,void 0,o,$)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(r=>r.hostUpdate?.()),this.update(t)):this._$EM()}catch(r){throw e=!1,this._$EM(),r}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&=this._$Eq.forEach(t=>this._$ET(t,this[t])),this._$EM()}updated(e){}firstUpdated(e){}};H.elementStyles=[],H.shadowRootOptions={mode:"open"},H[F("elementProperties")]=new Map,H[F("finalized")]=new Map,rt?.({ReactiveElement:H}),(ee.reactiveElementVersions??=[]).push("2.1.2");var he=globalThis,Te=n=>n,te=he.trustedTypes,Le=te?te.createPolicy("lit-html",{createHTML:n=>n}):void 0,Ie="$lit$",R=`lit$${Math.random().toFixed(9).slice(2)}$`,Ue="?"+R,it=`<${Ue}>`,O=document,K=()=>O.createComment(""),J=n=>n===null||typeof n!="object"&&typeof n!="function",pe=Array.isArray,nt=n=>pe(n)||typeof n?.[Symbol.iterator]=="function",ae=`[ 	
\f\r]`,W=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,He=/-->/g,Re=/>/g,q=RegExp(`>|${ae}(?:([^\\s"'>=/]+)(${ae}*=${ae}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),qe=/'/g,Ne=/"/g,Be=/^(?:script|style|textarea|title)$/i,fe=n=>(e,...t)=>({_$litType$:n,strings:e,values:t}),M=fe(1),vt=fe(2),bt=fe(3),I=Symbol.for("lit-noChange"),P=Symbol.for("lit-nothing"),Oe=new WeakMap,N=O.createTreeWalker(O,129);function ze(n,e){if(!pe(n)||!n.hasOwnProperty("raw"))throw Error("invalid template strings array");return Le!==void 0?Le.createHTML(e):e}var st=(n,e)=>{let t=n.length-1,r=[],i,o=e===2?"<svg>":e===3?"<math>":"",d=W;for(let $=0;$<t;$++){let m=n[$],A,x,E=-1,p=0;for(;p<m.length&&(d.lastIndex=p,x=d.exec(m),x!==null);)p=d.lastIndex,d===W?x[1]==="!--"?d=He:x[1]!==void 0?d=Re:x[2]!==void 0?(Be.test(x[2])&&(i=RegExp("</"+x[2],"g")),d=q):x[3]!==void 0&&(d=q):d===q?x[0]===">"?(d=i??W,E=-1):x[1]===void 0?E=-2:(E=d.lastIndex-x[2].length,A=x[1],d=x[3]===void 0?q:x[3]==='"'?Ne:qe):d===Ne||d===qe?d=q:d===He||d===Re?d=W:(d=q,i=void 0);let _=d===q&&n[$+1].startsWith("/>")?" ":"";o+=d===W?m+it:E>=0?(r.push(A),m.slice(0,E)+Ie+m.slice(E)+R+_):m+R+(E===-2?$:_)}return[ze(n,o+(n[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),r]},G=class n{constructor({strings:e,_$litType$:t},r){let i;this.parts=[];let o=0,d=0,$=e.length-1,m=this.parts,[A,x]=st(e,t);if(this.el=n.createElement(A,r),N.currentNode=this.el.content,t===2||t===3){let E=this.el.content.firstChild;E.replaceWith(...E.childNodes)}for(;(i=N.nextNode())!==null&&m.length<$;){if(i.nodeType===1){if(i.hasAttributes())for(let E of i.getAttributeNames())if(E.endsWith(Ie)){let p=x[d++],_=i.getAttribute(E).split(R),v=/([.?@])?(.*)/.exec(p);m.push({type:1,index:o,name:v[2],strings:_,ctor:v[1]==="."?le:v[1]==="?"?de:v[1]==="@"?ue:B}),i.removeAttribute(E)}else E.startsWith(R)&&(m.push({type:6,index:o}),i.removeAttribute(E));if(Be.test(i.tagName)){let E=i.textContent.split(R),p=E.length-1;if(p>0){i.textContent=te?te.emptyScript:"";for(let _=0;_<p;_++)i.append(E[_],K()),N.nextNode(),m.push({type:2,index:++o});i.append(E[p],K())}}}else if(i.nodeType===8)if(i.data===Ue)m.push({type:2,index:o});else{let E=-1;for(;(E=i.data.indexOf(R,E+1))!==-1;)m.push({type:7,index:o}),E+=R.length-1}o++}}static createElement(e,t){let r=O.createElement("template");return r.innerHTML=e,r}};function U(n,e,t=n,r){if(e===I)return e;let i=r!==void 0?t._$Co?.[r]:t._$Cl,o=J(e)?void 0:e._$litDirective$;return i?.constructor!==o&&(i?._$AO?.(!1),o===void 0?i=void 0:(i=new o(n),i._$AT(n,t,r)),r!==void 0?(t._$Co??=[])[r]=i:t._$Cl=i),i!==void 0&&(e=U(n,i._$AS(n,e.values),i,r)),e}var oe=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:r}=this._$AD,i=(e?.creationScope??O).importNode(t,!0);N.currentNode=i;let o=N.nextNode(),d=0,$=0,m=r[0];for(;m!==void 0;){if(d===m.index){let A;m.type===2?A=new Y(o,o.nextSibling,this,e):m.type===1?A=new m.ctor(o,m.name,m.strings,this,e):m.type===6&&(A=new ce(o,this,e)),this._$AV.push(A),m=r[++$]}d!==m?.index&&(o=N.nextNode(),d++)}return N.currentNode=O,i}p(e){let t=0;for(let r of this._$AV)r!==void 0&&(r.strings!==void 0?(r._$AI(e,r,t),t+=r.strings.length-2):r._$AI(e[t])),t++}},Y=class n{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,r,i){this.type=2,this._$AH=P,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=r,this.options=i,this._$Cv=i?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=U(this,e,t),J(e)?e===P||e==null||e===""?(this._$AH!==P&&this._$AR(),this._$AH=P):e!==this._$AH&&e!==I&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):nt(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==P&&J(this._$AH)?this._$AA.nextSibling.data=e:this.T(O.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:r}=e,i=typeof r=="number"?this._$AC(e):(r.el===void 0&&(r.el=G.createElement(ze(r.h,r.h[0]),this.options)),r);if(this._$AH?._$AD===i)this._$AH.p(t);else{let o=new oe(i,this),d=o.u(this.options);o.p(t),this.T(d),this._$AH=o}}_$AC(e){let t=Oe.get(e.strings);return t===void 0&&Oe.set(e.strings,t=new G(e)),t}k(e){pe(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,r,i=0;for(let o of e)i===t.length?t.push(r=new n(this.O(K()),this.O(K()),this,this.options)):r=t[i],r._$AI(o),i++;i<t.length&&(this._$AR(r&&r._$AB.nextSibling,i),t.length=i)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let r=Te(e).nextSibling;Te(e).remove(),e=r}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},B=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,r,i,o){this.type=1,this._$AH=P,this._$AN=void 0,this.element=e,this.name=t,this._$AM=i,this.options=o,r.length>2||r[0]!==""||r[1]!==""?(this._$AH=Array(r.length-1).fill(new String),this.strings=r):this._$AH=P}_$AI(e,t=this,r,i){let o=this.strings,d=!1;if(o===void 0)e=U(this,e,t,0),d=!J(e)||e!==this._$AH&&e!==I,d&&(this._$AH=e);else{let $=e,m,A;for(e=o[0],m=0;m<o.length-1;m++)A=U(this,$[r+m],t,m),A===I&&(A=this._$AH[m]),d||=!J(A)||A!==this._$AH[m],A===P?e=P:e!==P&&(e+=(A??"")+o[m+1]),this._$AH[m]=A}d&&!i&&this.j(e)}j(e){e===P?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},le=class extends B{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===P?void 0:e}},de=class extends B{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==P)}},ue=class extends B{constructor(e,t,r,i,o){super(e,t,r,i,o),this.type=5}_$AI(e,t=this){if((e=U(this,e,t,0)??P)===I)return;let r=this._$AH,i=e===P&&r!==P||e.capture!==r.capture||e.once!==r.once||e.passive!==r.passive,o=e!==P&&(r===P||i);i&&this.element.removeEventListener(this.name,this,r),o&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},ce=class{constructor(e,t,r){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=r}get _$AU(){return this._$AM._$AU}_$AI(e){U(this,e)}};var at=he.litHtmlPolyfillSupport;at?.(G,Y),(he.litHtmlVersions??=[]).push("3.3.2");var De=(n,e,t)=>{let r=t?.renderBefore??e,i=r._$litPart$;if(i===void 0){let o=t?.renderBefore??null;r._$litPart$=i=new Y(e.insertBefore(K(),o),o,void 0,t??{})}return i._$AI(n),i};var me=globalThis,T=class extends H{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let e=super.createRenderRoot();return this.renderOptions.renderBefore??=e.firstChild,e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=De(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return I}};T._$litElement$=!0,T.finalized=!0,me.litElementHydrateSupport?.({LitElement:T});var ot=me.litElementPolyfillSupport;ot?.({LitElement:T});(me.litElementVersions??=[]).push("4.2.2");ve();var be=class extends T{static properties={routes:{type:Object},queryParams:{type:Array},missingParams:{type:Array},currentPath:{type:String,attribute:"current-path"},_loading:{state:!0}};static styles=V`
    :host {
      width: var(--bino-sidebar-width);
      min-width: var(--bino-sidebar-width);
      background: var(--bino-surface);
      border-right: 1px solid var(--bino-border);
      padding: var(--bino-space-md);
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-md);
      overflow-y: auto;
      max-height: 100vh;
      position: sticky;
      top: 0;
      font-family: var(--bino-font-sans);
    }
    :host([hidden]) {
      display: none;
    }
    h3 {
      margin: 0;
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--bino-text-secondary);
    }
    .sitemap {
      border-bottom: 1px solid var(--bino-border);
      padding-bottom: var(--bino-space-md);
      margin-bottom: var(--bino-space-sm);
    }
    .route-list {
      list-style: none;
      margin: var(--bino-space-sm) 0 0 0;
      padding: 0;
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-xs);
    }
    .route-list li a {
      display: block;
      padding: var(--bino-space-sm) 0.75rem;
      border-radius: var(--bino-radius);
      text-decoration: none;
      color: var(--bino-text-muted);
      font-size: var(--bino-font-size-md);
      transition: background var(--bino-transition-fast);
    }
    .route-list li a:hover {
      background: var(--bino-surface-hover);
    }
    .route-list li.active a {
      background: var(--bino-surface-active);
      color: var(--bino-active-text);
      font-weight: 500;
    }
    .param-group {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
    }
    .param-group.missing {
      background: var(--bino-error-bg);
      border: 1px solid #fecaca;
      border-radius: 8px;
      padding: 0.75rem;
      margin: -0.25rem;
    }
    .param-group.missing .param-label {
      color: var(--bino-error);
    }
    .param-label {
      font-size: var(--bino-font-size-base);
      font-weight: 500;
      color: var(--bino-text-muted);
    }
    .param-label .required {
      color: var(--bino-error);
      margin-left: 2px;
    }
    .param-desc {
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      margin: 0;
    }
    .param-input {
      padding: var(--bino-space-sm) 0.75rem;
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-md);
      font-family: inherit;
      transition: border-color var(--bino-transition-fast), box-shadow var(--bino-transition-fast);
    }
    .param-input:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-primary-ring);
    }
    .param-input.invalid {
      border-color: var(--bino-error);
    }
    .param-select {
      appearance: none;
      background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%236b7280' d='M3 5l3 3 3-3'/%3E%3C/svg%3E");
      background-repeat: no-repeat;
      background-position: right 0.75rem center;
      padding-right: 2rem;
      cursor: pointer;
    }
    .range-slider-container {
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-sm);
    }
    .range-values {
      display: flex;
      justify-content: space-between;
      align-items: center;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text-muted);
      font-weight: 500;
    }
    .range-value {
      background: var(--bino-surface-hover);
      padding: var(--bino-space-xs) var(--bino-space-sm);
      border-radius: 4px;
      min-width: 3rem;
      text-align: center;
    }
    .range-sep {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-md);
    }
    .dual-range {
      position: relative;
      height: 1.5rem;
    }
    .range-slider {
      position: absolute;
      width: 100%;
      height: 6px;
      top: 50%;
      transform: translateY(-50%);
      -webkit-appearance: none;
      appearance: none;
      background: transparent;
      pointer-events: none;
      padding: 0;
      border: none;
      margin: 0;
    }
    .range-min { z-index: 1; }
    .range-max { z-index: 2; }
    .range-slider::-webkit-slider-runnable-track {
      width: 100%;
      height: 6px;
      background: var(--bino-border);
      border-radius: 3px;
    }
    .range-slider::-webkit-slider-thumb {
      -webkit-appearance: none;
      appearance: none;
      width: 18px;
      height: 18px;
      background: var(--bino-surface);
      border: 2px solid var(--bino-primary);
      border-radius: 50%;
      cursor: pointer;
      pointer-events: auto;
      margin-top: -6px;
      box-shadow: var(--bino-shadow-header);
    }
    .range-slider::-moz-range-track {
      width: 100%;
      height: 6px;
      background: var(--bino-border);
      border-radius: 3px;
    }
    .range-slider::-moz-range-thumb {
      width: 14px;
      height: 14px;
      background: var(--bino-surface);
      border: 2px solid var(--bino-primary);
      border-radius: 50%;
      cursor: pointer;
      pointer-events: auto;
      box-shadow: var(--bino-shadow-header);
    }
    input[type="date"].param-input,
    input[type="datetime-local"].param-input {
      cursor: pointer;
    }
    input[type="number"].param-input {
      -moz-appearance: textfield;
    }
    input[type="number"].param-input::-webkit-outer-spin-button,
    input[type="number"].param-input::-webkit-inner-spin-button {
      -webkit-appearance: none;
      margin: 0;
    }
    .apply-btn {
      padding: 0.625rem var(--bino-space-md);
      background: var(--bino-primary);
      color: var(--bino-surface);
      border: none;
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-md);
      font-weight: 500;
      cursor: pointer;
      transition: background var(--bino-transition-fast);
    }
    .apply-btn:hover {
      background: var(--bino-primary-hover);
    }
    .apply-btn:disabled {
      background: #9ca3af;
      cursor: not-allowed;
    }
    @media print {
      :host {
        display: none !important;
      }
    }
  `;constructor(){super(),this.routes={},this.queryParams=[],this.missingParams=[],this.currentPath="/",this._loading=!1}updateConfig(e){e.routes!==void 0&&(this.routes=e.routes),e.queryParams!==void 0&&(this.queryParams=e.queryParams),e.missingParams!==void 0&&(this.missingParams=e.missingParams),e.currentPath!==void 0&&(this.currentPath=e.currentPath)}setLoading(e){this._loading=e}render(){var e=Object.keys(this.routes).sort(),t=e.length>0,r=this.queryParams.length>0;this.hidden=!t&&!r;var i=new URLSearchParams(window.location.search);return M`
      ${t?this._renderNavigation(e):""}
      ${r?this._renderParams(i):""}
    `}_renderNavigation(e){return M`
      <div class="sitemap">
        <h3>Navigation</h3>
        <ul class="route-list">
          ${e.map(t=>{var r=this.routes[t]||t,i=t===this.currentPath;return M`
              <li class=${i?"active":""}>
                <a href=${t} @click=${o=>this._onRouteClick(o,t)}>${r}</a>
              </li>
            `})}
        </ul>
      </div>
    `}_renderParams(e){return M`
      <h3>Parameters</h3>
      ${this.queryParams.map(t=>this._renderParamGroup(t,e))}
      <button type="button" class="apply-btn"
        ?disabled=${this._loading}
        @click=${this._onApply}>
        ${this._loading?"Loading...":"Apply"}
      </button>
    `}_renderParamGroup(e,t){var r=t.get(e.name),i=null;e.type==="number_range"&&(i=t.get(e.name+"_max")),r===null&&e.default!==void 0&&e.default!==null&&(r=e.default),r=r||"",i=i||"";var o=this.missingParams.indexOf(e.name)!==-1;return M`
      <div class="param-group ${o?"missing":""}">
        <label class="param-label" for="param-${e.name}">
          ${e.name}${e.required?M`<span class="required">*</span>`:""}
        </label>
        ${e.description?M`<p class="param-desc">${e.description}</p>`:""}
        ${this._buildInput(e,r,i,o)}
      </div>
    `}_buildInput(e,t,r,i){var o=e.type||"string",d=e.options||{},$=i?" invalid":"";switch(o){case"number":return M`<input type="number"
          class="param-input${$}" id="param-${e.name}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          min=${d.min??""} max=${d.max??""} step=${d.step??""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;case"number_range":{var m=d.min!==void 0?d.min:0,A=d.max!==void 0?d.max:100,x=d.step!==void 0?d.step:1,E=t!==""?parseFloat(t):m,p=r!==""?parseFloat(r):A;return M`
          <div class="range-slider-container">
            <div class="range-values">
              <span class="range-value" id="range-min-${e.name}">${E}</span>
              <span class="range-sep">\u2013</span>
              <span class="range-value" id="range-max-${e.name}">${p}</span>
            </div>
            <div class="dual-range">
              <input type="range" class="param-input range-slider range-min${$}"
                name=${e.name} .value=${String(E)}
                min=${m} max=${A} step=${x}
                data-required=${e.required}
                @input=${this._onRangeInput}>
              <input type="range" class="param-input range-slider range-max"
                name="${e.name}_max" .value=${String(p)}
                min=${m} max=${A} step=${x}
                data-required="false"
                @input=${this._onRangeInput}>
            </div>
          </div>`}case"select":return M`
          <select class="param-input param-select${$}"
            name=${e.name} data-required=${e.required}
            @keypress=${this._onKeypress} @input=${this._onInputChange}>
            ${e.required?"":M`<option value="">-- Select --</option>`}
            ${(d.items||[]).map(_=>M`
              <option value=${_.value} ?selected=${t===_.value}>
                ${_.label||_.value}
              </option>
            `)}
          </select>`;case"date":return M`<input type="date" class="param-input${$}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;case"date_time":return M`<input type="datetime-local" class="param-input${$}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;default:return M`<input type="text" class="param-input${$}"
          name=${e.name} .value=${t}
          data-required=${e.required}
          placeholder=${e.default!=null?String(e.default):""}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`}}updated(){this._setupRangeSliders()}_setupRangeSliders(){var e=this;this.renderRoot.querySelectorAll(".dual-range").forEach(function(t){var r=t.querySelector(".range-min"),i=t.querySelector(".range-max");if(!r||!i||r._rangeSetup)return;r._rangeSetup=!0;var o=e.renderRoot.getElementById("range-min-"+r.name),d=e.renderRoot.getElementById("range-max-"+i.name);function $(){var m=parseFloat(r.value),A=parseFloat(i.value);m>A&&(this===r?(r.value=A,m=A):(i.value=m,A=m)),o&&(o.textContent=r.value),d&&(d.textContent=i.value)}r.addEventListener("input",$),i.addEventListener("input",$)})}_onRouteClick(e,t){e.preventDefault(),this.dispatchEvent(new CustomEvent("bino-navigate",{detail:{path:t},bubbles:!0,composed:!0}))}_onKeypress(e){e.key==="Enter"&&this._onApply()}_onInputChange(e){var t=e.target;t.classList.remove("invalid");var r=t.closest(".param-group");r&&r.classList.remove("missing")}_onRangeInput(e){this._onInputChange(e)}_onApply(){var e=this.renderRoot.querySelectorAll(".param-input"),t=new URLSearchParams,r=!0;e.forEach(function(i){var o=i.name,d=i.value.trim(),$=i.dataset.required==="true";$&&!d?(i.classList.add("invalid"),r=!1):(i.classList.remove("invalid"),d&&t.set(o,d))}),r&&this.dispatchEvent(new CustomEvent("bino-apply-params",{detail:{params:t},bubbles:!0,composed:!0}))}};customElements.define("bino-control-panel",be);var D=window.__binoServeConfig||{},_e=D.routes||{},ye=D.queryParams||[],z=D.missingParams||[],Q=D.currentPath||"/",We=D.currentURL||"/",$e=D.initialContextBase64||"",Ae=class extends T{static styles=V`
    :host {
      display: flex;
      width: 100%;
      min-height: 100vh;
    }
    #outlet {
      flex: 1;
      min-width: 0;
    }
  `;render(){return M`
      <bino-control-panel></bino-control-panel>
      <div id="outlet"><slot></slot></div>
    `}firstUpdated(){this._controlPanel=this.renderRoot.querySelector("bino-control-panel"),this._outlet=this.renderRoot.getElementById("outlet"),this._controlPanel&&this._controlPanel.updateConfig({routes:_e,queryParams:ye,missingParams:z,currentPath:Q}),this.addEventListener("bino-apply-params",this._onApplyParams.bind(this)),this.addEventListener("bino-navigate",this._onNavigate.bind(this)),this._initContent()}_initContent(){if(z&&z.length>0){document.readyState==="loading"?document.addEventListener("DOMContentLoaded",this._showMissingParamsMessage.bind(this)):this._showMissingParamsMessage();return}var e=this;Promise.resolve().then(()=>(ve(),Fe)).then(function(t){t.waitForEngine().then(function(){e._injectInitialContent()})})}_injectInitialContent(){if($e){var e=re($e),t=new DOMParser;ge(e,t),$e=null}}_showMissingParamsMessage(){var e=document.querySelector("bn-context");e&&e.remove();var t=this.querySelector(".bino-missing-params-banner");t&&t.remove();var r=document.createElement("div");r.className="bino-missing-params-banner",r.innerHTML='<div class="bino-missing-icon">\u26A0</div><div class="bino-missing-text"><strong>Required parameters missing</strong><p>Please fill in the required fields marked with <span class="required">*</span> to view the report.</p></div>',this.appendChild(r)}_onApplyParams(e){var t=e.detail.params,r=Q,i=t.toString();i&&(r+="?"+i),this._navigateTo(r)}_onNavigate(e){var t=e.detail.path;this._navigateTo(t)}_navigateTo(e){history.pushState({url:e},"",e),this._loadContent(e)}_loadContent(e){var t=this,r=document.querySelector("bn-context");r&&(r.style.opacity="0.5"),this._controlPanel&&this._controlPanel.setLoading(!0);var i=new DOMParser;fetch(e,{headers:{"X-Requested-With":"bino-serve"}}).then(function(o){if(!o.ok)throw new Error("HTTP "+o.status);return o.text()}).then(function(o){var d=i.parseFromString(o,"text/html"),$=d.getElementById("bino-serve-config");if(!$){console.error("bino: no config script found in response"),t._controlPanel&&t._controlPanel.setLoading(!1);return}var m=$.textContent,A=m.match(/window\.__binoServeConfig\s*=\s*(\{[\s\S]*\})\s*;?\s*$/),x={};if(A&&A[1])try{x=JSON.parse(A[1])}catch(a){console.error("bino: failed to parse config",a)}var E=x.missingParams||[],p=x.queryParams||[],_=x.currentPath||Q,v=x.initialContextBase64||"";z=E,ye=p,Q=_;var S=document.querySelector("bn-context");if(v){var w=re(v),b=i.parseFromString(w,"text/html"),l=b.querySelector("bn-context");if(l)if(S)S.replaceWith(l);else{var h=t.querySelector(".bino-missing-params-banner");h&&h.remove(),t.appendChild(l)}}else z.length>0&&(S&&S.remove(),t._showMissingParamsMessage());var s=d.querySelector("title");s&&(document.title=s.textContent),t._controlPanel&&(t._controlPanel.updateConfig({routes:_e,queryParams:ye,missingParams:z,currentPath:Q}),t._controlPanel.setLoading(!1))}).catch(function(o){console.error("bino: navigation failed",o),t._controlPanel&&t._controlPanel.setLoading(!1),r&&(r.style.opacity="1"),alert("Failed to load: "+o.message)})}};customElements.define("bino-serve-shell",Ae);document.addEventListener("click",function(n){var e=n.target.closest("a[href]");if(e){var t=e.getAttribute("href");if(!(!t||t.startsWith("http")||t.startsWith("//")||t.startsWith("#"))){var r=new URL(t,window.location.origin),i=r.pathname;if(_e.hasOwnProperty(i)){n.preventDefault();var o=document.querySelector("bino-serve-shell");o&&o._navigateTo(i+r.search)}}}});window.addEventListener("popstate",function(n){if(n.state&&n.state.url){var e=document.querySelector("bino-serve-shell");e&&e._loadContent(n.state.url)}});history.state||history.replaceState({url:We},"",We);
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
