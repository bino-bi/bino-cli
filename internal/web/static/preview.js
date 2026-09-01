var Ve=(function(){"use strict";let s=()=>{},e={morphStyle:"outerHTML",callbacks:{beforeNodeAdded:s,afterNodeAdded:s,beforeNodeMorphed:s,afterNodeMorphed:s,beforeNodeRemoved:s,afterNodeRemoved:s,beforeAttributeUpdated:s},head:{style:"merge",shouldPreserve:v=>v.getAttribute("im-preserve")==="true",shouldReAppend:v=>v.getAttribute("im-re-append")==="true",shouldRemove:s,afterHeadMorphed:s},restoreFocus:!0};function t(v,$,x={}){v=y(v);let S=E($),C=p(v,S,x),g=n(C,()=>h(C,v,S,d=>d.morphStyle==="innerHTML"?(i(d,v,S),Array.from(v.childNodes)):r(d,v,S)));return C.pantry.remove(),g}function r(v,$,x){let S=E($);return i(v,S,x,$,$.nextSibling),Array.from(S.childNodes)}function n(v,$){if(!v.config.restoreFocus)return $();let x=document.activeElement;if(!(x instanceof HTMLInputElement||x instanceof HTMLTextAreaElement))return $();let{id:S,selectionStart:C,selectionEnd:g}=x,d=$();return S&&S!==document.activeElement?.getAttribute("id")&&(x=v.target.querySelector(`[id="${S}"]`),x?.focus()),x&&!x.selectionEnd&&g&&x.setSelectionRange(C,g),d}let i=(function(){function v(a,l,f,b=null,_=null){l instanceof HTMLTemplateElement&&f instanceof HTMLTemplateElement&&(l=l.content,f=f.content),b||=l.firstChild;for(let w of f.childNodes){if(b&&b!=_){let O=x(a,w,b,_);if(O){O!==b&&C(a,b,O),o(O,w,a),b=O.nextSibling;continue}}if(w instanceof Element){let O=w.getAttribute("id");if(a.persistentIds.has(O)){let R=g(l,O,b,a);o(R,w,a),b=R.nextSibling;continue}}let A=$(l,w,b,a);A&&(b=A.nextSibling)}for(;b&&b!=_;){let w=b;b=b.nextSibling,S(a,w)}}function $(a,l,f,b){if(b.callbacks.beforeNodeAdded(l)===!1)return null;if(b.idMap.has(l)){let _=document.createElement(l.tagName);return a.insertBefore(_,f),o(_,l,b),b.callbacks.afterNodeAdded(_),_}else{let _=document.importNode(l,!0);return a.insertBefore(_,f),b.callbacks.afterNodeAdded(_),_}}let x=(function(){function a(b,_,w,A){let O=null,R=_.nextSibling,je=0,M=w;for(;M&&M!=A;){if(f(M,_)){if(l(b,M,_))return M;O===null&&(b.idMap.has(M)||(O=M))}if(O===null&&R&&f(M,R)&&(je++,R=R.nextSibling,je>=2&&(O=void 0)),b.activeElementAndParents.includes(M))break;M=M.nextSibling}return O||null}function l(b,_,w){let A=b.idMap.get(_),O=b.idMap.get(w);if(!O||!A)return!1;for(let R of A)if(O.has(R))return!0;return!1}function f(b,_){let w=b,A=_;return w.nodeType===A.nodeType&&w.tagName===A.tagName&&(!w.getAttribute?.("id")||w.getAttribute?.("id")===A.getAttribute?.("id"))}return a})();function S(a,l){if(a.idMap.has(l))m(a.pantry,l,null);else{if(a.callbacks.beforeNodeRemoved(l)===!1)return;l.parentNode?.removeChild(l),a.callbacks.afterNodeRemoved(l)}}function C(a,l,f){let b=l;for(;b&&b!==f;){let _=b;b=b.nextSibling,S(a,_)}return b}function g(a,l,f,b){let _=b.target.getAttribute?.("id")===l&&b.target||b.target.querySelector(`[id="${l}"]`)||b.pantry.querySelector(`[id="${l}"]`);return d(_,b),m(a,_,f),_}function d(a,l){let f=a.getAttribute("id");for(;a=a.parentNode;){let b=l.idMap.get(a);b&&(b.delete(f),b.size||l.idMap.delete(a))}}function m(a,l,f){if(a.moveBefore)try{a.moveBefore(l,f)}catch{a.insertBefore(l,f)}else a.insertBefore(l,f)}return v})(),o=(function(){function v(d,m,a){return a.ignoreActive&&d===document.activeElement?null:(a.callbacks.beforeNodeMorphed(d,m)===!1||(d instanceof HTMLHeadElement&&a.head.ignore||(d instanceof HTMLHeadElement&&a.head.style!=="morph"?u(d,m,a):($(d,m,a),g(d,a)||i(a,d,m))),a.callbacks.afterNodeMorphed(d,m)),d)}function $(d,m,a){let l=m.nodeType;if(l===1){let f=d,b=m,_=f.attributes,w=b.attributes;for(let A of w)C(A.name,f,"update",a)||f.getAttribute(A.name)!==A.value&&f.setAttribute(A.name,A.value);for(let A=_.length-1;0<=A;A--){let O=_[A];if(O&&!b.hasAttribute(O.name)){if(C(O.name,f,"remove",a))continue;f.removeAttribute(O.name)}}g(f,a)||x(f,b,a)}(l===8||l===3)&&d.nodeValue!==m.nodeValue&&(d.nodeValue=m.nodeValue)}function x(d,m,a){if(d instanceof HTMLInputElement&&m instanceof HTMLInputElement&&m.type!=="file"){let l=m.value,f=d.value;S(d,m,"checked",a),S(d,m,"disabled",a),m.hasAttribute("value")?f!==l&&(C("value",d,"update",a)||(d.setAttribute("value",l),d.value=l)):C("value",d,"remove",a)||(d.value="",d.removeAttribute("value"))}else if(d instanceof HTMLOptionElement&&m instanceof HTMLOptionElement)S(d,m,"selected",a);else if(d instanceof HTMLTextAreaElement&&m instanceof HTMLTextAreaElement){let l=m.value,f=d.value;if(C("value",d,"update",a))return;l!==f&&(d.value=l),d.firstChild&&d.firstChild.nodeValue!==l&&(d.firstChild.nodeValue=l)}}function S(d,m,a,l){let f=m[a],b=d[a];if(f!==b){let _=C(a,d,"update",l);_||(d[a]=m[a]),f?_||d.setAttribute(a,""):C(a,d,"remove",l)||d.removeAttribute(a)}}function C(d,m,a,l){return d==="value"&&l.ignoreActiveValue&&m===document.activeElement?!0:l.callbacks.beforeAttributeUpdated(d,m,a)===!1}function g(d,m){return!!m.ignoreActiveValue&&d===document.activeElement&&d!==document.body}return v})();function h(v,$,x,S){if(v.head.block){let C=$.querySelector("head"),g=x.querySelector("head");if(C&&g){let d=u(C,g,v);return Promise.all(d).then(()=>{let m=Object.assign(v,{head:{block:!1,ignore:!0}});return S(m)})}}return S(v)}function u(v,$,x){let S=[],C=[],g=[],d=[],m=new Map;for(let l of $.children)m.set(l.outerHTML,l);for(let l of v.children){let f=m.has(l.outerHTML),b=x.head.shouldReAppend(l),_=x.head.shouldPreserve(l);f||_?b?C.push(l):(m.delete(l.outerHTML),g.push(l)):x.head.style==="append"?b&&(C.push(l),d.push(l)):x.head.shouldRemove(l)!==!1&&C.push(l)}d.push(...m.values());let a=[];for(let l of d){let f=document.createRange().createContextualFragment(l.outerHTML).firstChild;if(x.callbacks.beforeNodeAdded(f)!==!1){if("href"in f&&f.href||"src"in f&&f.src){let b,_=new Promise(function(w){b=w});f.addEventListener("load",function(){b()}),a.push(_)}v.appendChild(f),x.callbacks.afterNodeAdded(f),S.push(f)}}for(let l of C)x.callbacks.beforeNodeRemoved(l)!==!1&&(v.removeChild(l),x.callbacks.afterNodeRemoved(l));return x.head.afterHeadMorphed(v,{added:S,kept:g,removed:C}),a}let p=(function(){function v(a,l,f){let{persistentIds:b,idMap:_}=d(a,l),w=$(f),A=w.morphStyle||"outerHTML";if(!["innerHTML","outerHTML"].includes(A))throw`Do not understand how to morph style ${A}`;return{target:a,newContent:l,config:w,morphStyle:A,ignoreActive:w.ignoreActive,ignoreActiveValue:w.ignoreActiveValue,restoreFocus:w.restoreFocus,idMap:_,persistentIds:b,pantry:x(),activeElementAndParents:S(a),callbacks:w.callbacks,head:w.head}}function $(a){let l=Object.assign({},e);return Object.assign(l,a),l.callbacks=Object.assign({},e.callbacks,a.callbacks),l.head=Object.assign({},e.head,a.head),l}function x(){let a=document.createElement("div");return a.hidden=!0,document.body.insertAdjacentElement("afterend",a),a}function S(a){let l=[],f=document.activeElement;if(f?.tagName!=="BODY"&&a.contains(f))for(;f&&(l.push(f),f!==a);)f=f.parentElement;return l}function C(a){let l=Array.from(a.querySelectorAll("[id]"));return a.getAttribute?.("id")&&l.push(a),l}function g(a,l,f,b){for(let _ of b){let w=_.getAttribute("id");if(l.has(w)){let A=_;for(;A;){let O=a.get(A);if(O==null&&(O=new Set,a.set(A,O)),O.add(w),A===f)break;A=A.parentElement}}}}function d(a,l){let f=C(a),b=C(l),_=m(f,b),w=new Map;g(w,_,a,f);let A=l.__idiomorphRoot||l;return g(w,_,A,b),{persistentIds:_,idMap:w}}function m(a,l){let f=new Set,b=new Map;for(let{id:w,tagName:A}of a)b.has(w)?f.add(w):b.set(w,A);let _=new Set;for(let{id:w,tagName:A}of l)_.has(w)?f.add(w):b.get(w)===A&&_.add(w);for(let w of f)_.delete(w);return _}return v})(),{normalizeElement:y,normalizeParent:E}=(function(){let v=new WeakSet;function $(g){return g instanceof Document?g.documentElement:g}function x(g){if(g==null)return document.createElement("div");if(typeof g=="string")return x(C(g));if(v.has(g))return g;if(g instanceof Node){if(g.parentNode)return new S(g);{let d=document.createElement("div");return d.append(g),d}}else{let d=document.createElement("div");for(let m of[...g])d.append(m);return d}}class S{constructor(d){this.originalNode=d,this.realParentNode=d.parentNode,this.previousSibling=d.previousSibling,this.nextSibling=d.nextSibling}get childNodes(){let d=[],m=this.previousSibling?this.previousSibling.nextSibling:this.realParentNode.firstChild;for(;m&&m!=this.nextSibling;)d.push(m),m=m.nextSibling;return d}querySelectorAll(d){return this.childNodes.reduce((m,a)=>{if(a instanceof Element){a.matches(d)&&m.push(a);let l=a.querySelectorAll(d);for(let f=0;f<l.length;f++)m.push(l[f])}return m},[])}insertBefore(d,m){return this.realParentNode.insertBefore(d,m)}moveBefore(d,m){return this.realParentNode.moveBefore(d,m)}get __idiomorphRoot(){return this.originalNode}}function C(g){let d=new DOMParser,m=g.replace(/<svg(\s[^>]*>|>)([\s\S]*?)<\/svg>/gim,"");if(m.match(/<\/html>/)||m.match(/<\/head>/)||m.match(/<\/body>/)){let a=d.parseFromString(g,"text/html");if(m.match(/<\/html>/))return v.add(a),a;{let l=a.firstChild;return l&&v.add(l),l}}else{let l=d.parseFromString("<body><template>"+g+"</template></body>","text/html").body.querySelector("template").content;return v.add(l),l}}return{normalizeElement:$,normalizeParent:x}})();return{morph:t,defaults:e}})();function W(s){return s?s.charAt(0)==="/"?s:"/"+s:"/"}function T(){var s=document.querySelector("base");if(!s||!s.href)return"";var e=new URL(s.href,window.location.href).pathname;return e==="/"?"":e.replace(/\/+$/,"")}function ie(){var s=T(),e=window.location.pathname||"/";return s&&(e===s||e.indexOf(s+"/")===0)&&(e=e.slice(s.length)),W(e)}function Fe(){return customElements.get("bn-context")?Promise.resolve():customElements.whenDefined("bn-context")}function We(s,e){if(!s)return console.debug("bino: swapContext skipped \u2014 empty html"),!1;e||(e=new DOMParser);var t=e.parseFromString(s,"text/html"),r=t.querySelector("bn-context"),n=document.querySelector("bn-context");return r?n?(xt(n,r),Ve.morph(n,r.innerHTML,{morphStyle:"innerHTML",callbacks:{beforeAttributeUpdated:function(i,o,h){if(i==="class"&&o.tagName&&o.tagName.includes("-"))return!1}}}),!0):(console.debug("bino: swapContext skipped \u2014 live DOM has no <bn-context>"),!1):(console.debug("bino: swapContext skipped \u2014 incoming HTML has no <bn-context>"),!1)}function xt(s,e){for(var t=0;t<e.attributes.length;t++){var r=e.attributes[t];r.name!=="class"&&s.getAttribute(r.name)!==r.value&&s.setAttribute(r.name,r.value)}for(var n=s.attributes.length-1;n>=0;n--){var i=s.attributes[n].name;i!=="class"&&(e.hasAttribute(i)||s.removeAttribute(i))}}var se=globalThis,oe=se.ShadowRoot&&(se.ShadyCSS===void 0||se.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,fe=Symbol(),Qe=new WeakMap,Q=class{constructor(e,t,r){if(this._$cssResult$=!0,r!==fe)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(oe&&e===void 0){let r=t!==void 0&&t.length===1;r&&(e=Qe.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),r&&Qe.set(t,e))}return e}toString(){return this.cssText}},Je=s=>new Q(typeof s=="string"?s:s+"",void 0,fe),L=(s,...e)=>{let t=s.length===1?s[0]:e.reduce((r,n,i)=>r+(o=>{if(o._$cssResult$===!0)return o.cssText;if(typeof o=="number")return o;throw Error("Value passed to 'css' function must be a 'css' function result: "+o+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(n)+s[i+1],s[0]);return new Q(t,s,fe)},Ye=(s,e)=>{if(oe)s.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let r=document.createElement("style"),n=se.litNonce;n!==void 0&&r.setAttribute("nonce",n),r.textContent=t.cssText,s.appendChild(r)}},ve=oe?s=>s:s=>s instanceof CSSStyleSheet?(e=>{let t="";for(let r of e.cssRules)t+=r.cssText;return Je(t)})(s):s;var{is:wt,defineProperty:$t,getOwnPropertyDescriptor:kt,getOwnPropertyNames:Et,getOwnPropertySymbols:St,getPrototypeOf:At}=Object,ae=globalThis,Ge=ae.trustedTypes,Ct=Ge?Ge.emptyScript:"",Ot=ae.reactiveElementPolyfillSupport,J=(s,e)=>s,me={toAttribute(s,e){switch(e){case Boolean:s=s?Ct:null;break;case Object:case Array:s=s==null?s:JSON.stringify(s)}return s},fromAttribute(s,e){let t=s;switch(e){case Boolean:t=s!==null;break;case Number:t=s===null?null:Number(s);break;case Object:case Array:try{t=JSON.parse(s)}catch{t=null}}return t}},Ze=(s,e)=>!wt(s,e),Xe={attribute:!0,type:String,converter:me,reflect:!1,useDefault:!1,hasChanged:Ze};Symbol.metadata??=Symbol("metadata"),ae.litPropertyMetadata??=new WeakMap;var D=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??=[]).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=Xe){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let r=Symbol(),n=this.getPropertyDescriptor(e,r,t);n!==void 0&&$t(this.prototype,e,n)}}static getPropertyDescriptor(e,t,r){let{get:n,set:i}=kt(this.prototype,e)??{get(){return this[t]},set(o){this[t]=o}};return{get:n,set(o){let h=n?.call(this);i?.call(this,o),this.requestUpdate(e,h,r)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??Xe}static _$Ei(){if(this.hasOwnProperty(J("elementProperties")))return;let e=At(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(J("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(J("properties"))){let t=this.properties,r=[...Et(t),...St(t)];for(let n of r)this.createProperty(n,t[n])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[r,n]of t)this.elementProperties.set(r,n)}this._$Eh=new Map;for(let[t,r]of this.elementProperties){let n=this._$Eu(t,r);n!==void 0&&this._$Eh.set(n,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let r=new Set(e.flat(1/0).reverse());for(let n of r)t.unshift(ve(n))}else e!==void 0&&t.push(ve(e));return t}static _$Eu(e,t){let r=t.attribute;return r===!1?void 0:typeof r=="string"?r:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??=new Set).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let r of t.keys())this.hasOwnProperty(r)&&(e.set(r,this[r]),delete this[r]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return Ye(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,r){this._$AK(e,r)}_$ET(e,t){let r=this.constructor.elementProperties.get(e),n=this.constructor._$Eu(e,r);if(n!==void 0&&r.reflect===!0){let i=(r.converter?.toAttribute!==void 0?r.converter:me).toAttribute(t,r.type);this._$Em=e,i==null?this.removeAttribute(n):this.setAttribute(n,i),this._$Em=null}}_$AK(e,t){let r=this.constructor,n=r._$Eh.get(e);if(n!==void 0&&this._$Em!==n){let i=r.getPropertyOptions(n),o=typeof i.converter=="function"?{fromAttribute:i.converter}:i.converter?.fromAttribute!==void 0?i.converter:me;this._$Em=n;let h=o.fromAttribute(t,i.type);this[n]=h??this._$Ej?.get(n)??h,this._$Em=null}}requestUpdate(e,t,r,n=!1,i){if(e!==void 0){let o=this.constructor;if(n===!1&&(i=this[e]),r??=o.getPropertyOptions(e),!((r.hasChanged??Ze)(i,t)||r.useDefault&&r.reflect&&i===this._$Ej?.get(e)&&!this.hasAttribute(o._$Eu(e,r))))return;this.C(e,t,r)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:r,reflect:n,wrapped:i},o){r&&!(this._$Ej??=new Map).has(e)&&(this._$Ej.set(e,o??t??this[e]),i!==!0||o!==void 0)||(this._$AL.has(e)||(this.hasUpdated||r||(t=void 0),this._$AL.set(e,t)),n===!0&&this._$Em!==e&&(this._$Eq??=new Set).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[n,i]of this._$Ep)this[n]=i;this._$Ep=void 0}let r=this.constructor.elementProperties;if(r.size>0)for(let[n,i]of r){let{wrapped:o}=i,h=this[n];o!==!0||this._$AL.has(n)||h===void 0||this.C(n,void 0,i,h)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(r=>r.hostUpdate?.()),this.update(t)):this._$EM()}catch(r){throw e=!1,this._$EM(),r}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&=this._$Eq.forEach(t=>this._$ET(t,this[t])),this._$EM()}updated(e){}firstUpdated(e){}};D.elementStyles=[],D.shadowRootOptions={mode:"open"},D[J("elementProperties")]=new Map,D[J("finalized")]=new Map,Ot?.({ReactiveElement:D}),(ae.reactiveElementVersions??=[]).push("2.1.2");var ke=globalThis,et=s=>s,le=ke.trustedTypes,tt=le?le.createPolicy("lit-html",{createHTML:s=>s}):void 0,at="$lit$",H=`lit$${Math.random().toFixed(9).slice(2)}$`,lt="?"+H,zt=`<${lt}>`,q=document,G=()=>q.createComment(""),X=s=>s===null||typeof s!="object"&&typeof s!="function",Ee=Array.isArray,Lt=s=>Ee(s)||typeof s?.[Symbol.iterator]=="function",ge=`[ 	
\f\r]`,Y=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,rt=/-->/g,nt=/>/g,I=RegExp(`>|${ge}(?:([^\\s"'>=/]+)(${ge}*=${ge}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),it=/'/g,st=/"/g,dt=/^(?:script|style|textarea|title)$/i,Se=s=>(e,...t)=>({_$litType$:s,strings:e,values:t}),c=Se(1),Ae=Se(2),ir=Se(3),U=Symbol.for("lit-noChange"),k=Symbol.for("lit-nothing"),ot=new WeakMap,P=q.createTreeWalker(q,129);function ct(s,e){if(!Ee(s)||!s.hasOwnProperty("raw"))throw Error("invalid template strings array");return tt!==void 0?tt.createHTML(e):e}var Tt=(s,e)=>{let t=s.length-1,r=[],n,i=e===2?"<svg>":e===3?"<math>":"",o=Y;for(let h=0;h<t;h++){let u=s[h],p,y,E=-1,v=0;for(;v<u.length&&(o.lastIndex=v,y=o.exec(u),y!==null);)v=o.lastIndex,o===Y?y[1]==="!--"?o=rt:y[1]!==void 0?o=nt:y[2]!==void 0?(dt.test(y[2])&&(n=RegExp("</"+y[2],"g")),o=I):y[3]!==void 0&&(o=I):o===I?y[0]===">"?(o=n??Y,E=-1):y[1]===void 0?E=-2:(E=o.lastIndex-y[2].length,p=y[1],o=y[3]===void 0?I:y[3]==='"'?st:it):o===st||o===it?o=I:o===rt||o===nt?o=Y:(o=I,n=void 0);let $=o===I&&s[h+1].startsWith("/>")?" ":"";i+=o===Y?u+zt:E>=0?(r.push(p),u.slice(0,E)+at+u.slice(E)+H+$):u+H+(E===-2?h:$)}return[ct(s,i+(s[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),r]},Z=class s{constructor({strings:e,_$litType$:t},r){let n;this.parts=[];let i=0,o=0,h=e.length-1,u=this.parts,[p,y]=Tt(e,t);if(this.el=s.createElement(p,r),P.currentNode=this.el.content,t===2||t===3){let E=this.el.content.firstChild;E.replaceWith(...E.childNodes)}for(;(n=P.nextNode())!==null&&u.length<h;){if(n.nodeType===1){if(n.hasAttributes())for(let E of n.getAttributeNames())if(E.endsWith(at)){let v=y[o++],$=n.getAttribute(E).split(H),x=/([.?@])?(.*)/.exec(v);u.push({type:1,index:i,name:x[2],strings:$,ctor:x[1]==="."?ye:x[1]==="?"?xe:x[1]==="@"?we:F}),n.removeAttribute(E)}else E.startsWith(H)&&(u.push({type:6,index:i}),n.removeAttribute(E));if(dt.test(n.tagName)){let E=n.textContent.split(H),v=E.length-1;if(v>0){n.textContent=le?le.emptyScript:"";for(let $=0;$<v;$++)n.append(E[$],G()),P.nextNode(),u.push({type:2,index:++i});n.append(E[v],G())}}}else if(n.nodeType===8)if(n.data===lt)u.push({type:2,index:i});else{let E=-1;for(;(E=n.data.indexOf(H,E+1))!==-1;)u.push({type:7,index:i}),E+=H.length-1}i++}}static createElement(e,t){let r=q.createElement("template");return r.innerHTML=e,r}};function V(s,e,t=s,r){if(e===U)return e;let n=r!==void 0?t._$Co?.[r]:t._$Cl,i=X(e)?void 0:e._$litDirective$;return n?.constructor!==i&&(n?._$AO?.(!1),i===void 0?n=void 0:(n=new i(s),n._$AT(s,t,r)),r!==void 0?(t._$Co??=[])[r]=n:t._$Cl=n),n!==void 0&&(e=V(s,n._$AS(s,e.values),n,r)),e}var _e=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:r}=this._$AD,n=(e?.creationScope??q).importNode(t,!0);P.currentNode=n;let i=P.nextNode(),o=0,h=0,u=r[0];for(;u!==void 0;){if(o===u.index){let p;u.type===2?p=new ee(i,i.nextSibling,this,e):u.type===1?p=new u.ctor(i,u.name,u.strings,this,e):u.type===6&&(p=new $e(i,this,e)),this._$AV.push(p),u=r[++h]}o!==u?.index&&(i=P.nextNode(),o++)}return P.currentNode=q,n}p(e){let t=0;for(let r of this._$AV)r!==void 0&&(r.strings!==void 0?(r._$AI(e,r,t),t+=r.strings.length-2):r._$AI(e[t])),t++}},ee=class s{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,r,n){this.type=2,this._$AH=k,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=r,this.options=n,this._$Cv=n?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=V(this,e,t),X(e)?e===k||e==null||e===""?(this._$AH!==k&&this._$AR(),this._$AH=k):e!==this._$AH&&e!==U&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):Lt(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==k&&X(this._$AH)?this._$AA.nextSibling.data=e:this.T(q.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:r}=e,n=typeof r=="number"?this._$AC(e):(r.el===void 0&&(r.el=Z.createElement(ct(r.h,r.h[0]),this.options)),r);if(this._$AH?._$AD===n)this._$AH.p(t);else{let i=new _e(n,this),o=i.u(this.options);i.p(t),this.T(o),this._$AH=i}}_$AC(e){let t=ot.get(e.strings);return t===void 0&&ot.set(e.strings,t=new Z(e)),t}k(e){Ee(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,r,n=0;for(let i of e)n===t.length?t.push(r=new s(this.O(G()),this.O(G()),this,this.options)):r=t[n],r._$AI(i),n++;n<t.length&&(this._$AR(r&&r._$AB.nextSibling,n),t.length=n)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let r=et(e).nextSibling;et(e).remove(),e=r}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},F=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,r,n,i){this.type=1,this._$AH=k,this._$AN=void 0,this.element=e,this.name=t,this._$AM=n,this.options=i,r.length>2||r[0]!==""||r[1]!==""?(this._$AH=Array(r.length-1).fill(new String),this.strings=r):this._$AH=k}_$AI(e,t=this,r,n){let i=this.strings,o=!1;if(i===void 0)e=V(this,e,t,0),o=!X(e)||e!==this._$AH&&e!==U,o&&(this._$AH=e);else{let h=e,u,p;for(e=i[0],u=0;u<i.length-1;u++)p=V(this,h[r+u],t,u),p===U&&(p=this._$AH[u]),o||=!X(p)||p!==this._$AH[u],p===k?e=k:e!==k&&(e+=(p??"")+i[u+1]),this._$AH[u]=p}o&&!n&&this.j(e)}j(e){e===k?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},ye=class extends F{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===k?void 0:e}},xe=class extends F{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==k)}},we=class extends F{constructor(e,t,r,n,i){super(e,t,r,n,i),this.type=5}_$AI(e,t=this){if((e=V(this,e,t,0)??k)===U)return;let r=this._$AH,n=e===k&&r!==k||e.capture!==r.capture||e.once!==r.once||e.passive!==r.passive,i=e!==k&&(r===k||n);n&&this.element.removeEventListener(this.name,this,r),i&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},$e=class{constructor(e,t,r){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=r}get _$AU(){return this._$AM._$AU}_$AI(e){V(this,e)}};var Mt=ke.litHtmlPolyfillSupport;Mt?.(Z,ee),(ke.litHtmlVersions??=[]).push("3.3.2");var ht=(s,e,t)=>{let r=t?.renderBefore??e,n=r._$litPart$;if(n===void 0){let i=t?.renderBefore??null;r._$litPart$=n=new ee(e.insertBefore(G(),i),i,void 0,t??{})}return n._$AI(s),n};var Ce=globalThis,z=class extends D{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let e=super.createRenderRoot();return this.renderOptions.renderBefore??=e.firstChild,e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=ht(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return U}};z._$litElement$=!0,z.finalized=!0,Ce.litElementHydrateSupport?.({LitElement:z});var Rt=Ce.litElementPolyfillSupport;Rt?.({LitElement:z});(Ce.litElementVersions??=[]).push("4.2.2");var pt="bn-body, bn-card, bn-chart-bubble, bn-chart-bullet, bn-chart-scatter, bn-chart-structure, bn-chart-time, bn-footer, bn-grid, bn-image, bn-layout-card, bn-layout-page, bn-message, bn-page, bn-table, bn-template, bn-text, bn-title, bn-tree";function Oe(){return document.querySelector("bn-context")}function de(){var s=Oe();return!!s&&typeof s.getLayoutState=="function"}function ut(s){return new Promise(function(e){setTimeout(e,s)})}function Dt(s){var e=[];return Array.prototype.forEach.call(s.querySelectorAll(pt),function(t){var r=t.getBoundingClientRect();e.push(Math.round(r.width)+"x"+Math.round(r.height))}),e.join(",")}var Nt=250,Ht=8e3;async function It(s){for(var e=Date.now()+(s||Ht),t=Oe();window.componentRegisterIsRenderedResult!==!0;){if(Date.now()>=e)return!1;await ut(100)}if(!t)return!0;for(var r=null;Date.now()<e;){var n=Dt(t);if(n===r)return!0;r=n,await ut(Nt)}return!1}function Pt(s){var e={};return Array.prototype.map.call(s.querySelectorAll(pt),function(t){var r=t.localName,n=e[r]||0;e[r]=n+1;var i=t.getAttribute("id")||t.getAttribute("name");return{id:i||r+"["+n+"]",element:t}})}function qt(s){var e={};return s.forEach(function(t){var r=t.element,n=r.closest?r.closest("[data-bino-kind]"):null,i={};n&&(i.kind=n.getAttribute("data-bino-kind")||"",i.name=n.getAttribute("data-bino-name")||"",i.ref=n.getAttribute("data-bino-ref")||"");var o=r.getAttribute("measure-unit"),h=r.getAttribute("measure-scale");o&&(i.measureUnit=o),h&&(i.measureScale=h),(i.kind||i.name||i.ref||i.measureUnit)&&(e[t.id]=i)}),e}async function bt(s){var e=Oe();if(!e||typeof e.getLayoutState!="function")return null;var t=await It(s&&s.settleTimeoutMs),r=await e.getLayoutState(),n=Pt(e),i=new Map;return n.forEach(function(o){i.set(o.id,o.element)}),{state:r,sources:qt(n),elements:i,settled:t}}async function ft(s){return!s||typeof s.getLayoutState!="function"?null:s.getLayoutState({detail:"full"})}var ze=class extends z{static properties={artifacts:{type:Array},documents:{type:Array},graph:{type:Object},currentPath:{type:String,attribute:"current-path"},_errorCount:{state:!0},_badgeVisible:{state:!0},_refreshing:{state:!0},_refreshError:{state:!0},_inspectorAvailable:{state:!0},_buildCopied:{state:!0}};static styles=L`
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
    .assets-btn:hover, .graph-btn:hover:not(:disabled), .explorer-btn:hover, .inspect-btn:hover:not(:disabled), .build-btn:hover {
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
    .graph-btn:disabled, .present-btn:disabled, .inspect-btn:disabled {
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
  `;constructor(){super(),this.artifacts=[],this.documents=[],this.graph=null,this.currentPath="/",this._errorCount=0,this._badgeVisible=!1,this._refreshing=!1,this._refreshError="",this._panelDismissed=!1,this._inspectorAvailable=!1,this._buildCopied=!1,this._boundOnContentUpdated=this._refreshInspectorAvailability.bind(this),this._boundOnErrorsChanged=this._onErrorsChanged.bind(this),this._boundOnPanelDismissed=this._onPanelDismissed.bind(this),this._boundOnRefreshing=this._onRefreshing.bind(this),this._boundOnRefreshDone=this._onRefreshDone.bind(this),this._boundOnRefreshError=this._onRefreshError.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.addEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.addEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.addEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.addEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._refreshInspectorAvailability()}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.removeEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.removeEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.removeEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.removeEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated)}render(){var e=this,t=this.currentPath||"/",r=this.artifacts||[],n=[],i=[];r.forEach(function(p){p.isDoc?i.push(p):n.push(p)});var o=t!=="/"&&!t.startsWith("/doc/")&&!t.startsWith("/pres/"),h=o?T()+"/pres"+t:null,u=i.find(function(p){return"/doc/"+p.name===t})||null;return c`
      <span class="title">
        <img class="mark" src=${T()+"/__bino/assets/bino-mark.png"} alt="">
        <span>bino preview</span>
      </span>
      <select id="artefact-select" @change=${this._onSelectChange}>
        <option value="/" ?selected=${t==="/"}>All Pages</option>
        ${n.length>0?c`
          <optgroup label="Report Artefacts">
            ${n.map(function(p){var y="/"+p.name,E=p.title?p.title+" ("+p.name+")":p.name;return c`<option value=${y} ?selected=${y===t}>${E}</option>`})}
          </optgroup>
        `:""}
        ${i.length>0?c`
          <optgroup label="Document Artefacts">
            ${i.map(function(p){var y="/doc/"+p.name,E=p.title?p.title+" ("+p.name+")":p.name;return c`<option value=${y} ?selected=${y===t}>${E}</option>`})}
          </optgroup>
        `:""}
      </select>
      ${u?c`
        <span class="doc-meta" title="Document settings from the manifest">${this._docMetaText(u)}</span>
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
        title=${this._inspectorAvailable?u?"Inspect embedded engine components":"Inspect the rendered report":"Requires template engine v1.0.0-next.24 or newer"}
        @click=${this._onInspectClick}>
        <span class="inspect-icon">\u25a3</span>
        <span>Inspect</span>
      </button>
      <button class="present-btn" ?disabled=${!h}
        title=${h?"Open presentation":"Presentation is only available for a report artefact"}
        @click=${function(){h&&window.open(h,"_blank")}}>
        <span class="present-icon">\u25B6</span>
        <span>Present</span>
      </button>
      ${u?c`
        <button class="build-btn"
          title=${'Copy "bino build --artefact '+u.name+'" to the clipboard \u2014 pagination, TOC page numbers and headers render only in the built PDF'}
          @click=${function(){e._onCopyBuildCmd(u.name)}}>
          <span class="build-icon">${this._buildCopied?"\u2713":"\u2193"}</span>
          <span>${this._buildCopied?"Copied":"Build PDF"}</span>
        </button>
      `:""}
      <span class="spacer"></span>
      ${this._refreshError?c`
        <span class="refresh-error-msg" title=${this._refreshError}>
          <span>⚠</span>
          <span>Refresh failed</span>
        </span>
      `:""}
      <slot></slot>
      <div class="progress-bar ${this._refreshError?"error":this._refreshing?"active":""}"></div>
    `}updated(e){e.has("documents")&&document.dispatchEvent(new CustomEvent("bino-documents-changed",{detail:{documents:this.documents||[]}}))}_onAssetsClick(){document.dispatchEvent(new CustomEvent("bino-open-assets",{detail:{documents:this.documents||[]}}))}_onGraphClick(){document.dispatchEvent(new CustomEvent("bino-open-graph",{detail:{graph:this.graph}}))}_onExplorerClick(){document.dispatchEvent(new CustomEvent("bino-open-explorer"))}_onInspectClick(){document.dispatchEvent(new CustomEvent("bino-open-inspector"))}_docMetaText(e){var t=[];return e.format&&t.push(e.format),e.orientation&&t.push(e.orientation),e.locale&&t.push(e.locale),e.chapters===1&&t.push("1 chapter"),e.chapters>1&&t.push(e.chapters+" chapters"),e.toc&&t.push("TOC"),e.headerFooter&&t.push("header/footer"),t.join(" \xB7 ")}_onCopyBuildCmd(e){var t=this,r="bino build --artefact "+e,n=function(){t._buildCopied=!0,setTimeout(function(){t._buildCopied=!1},2e3)},i=function(){var o=document.createElement("textarea");o.value=r,o.style.position="fixed",o.style.opacity="0",document.body.appendChild(o),o.select();try{document.execCommand("copy")&&n()}catch(h){console.warn("bino-toolbar: clipboard copy failed",h)}document.body.removeChild(o)};navigator.clipboard&&navigator.clipboard.writeText?navigator.clipboard.writeText(r).then(n,i):i()}_refreshInspectorAvailability(){this._inspectorAvailable=de()}_onSelectChange(e){var t=e.target.value;t&&(window.location.href=T()+t)}_onBadgeClick(){this._panelDismissed=!1,this._badgeVisible=!1,document.dispatchEvent(new CustomEvent("bino-show-errors"))}_onErrorsChanged(e){this._errorCount=e.detail&&e.detail.count||0,this._panelDismissed&&this._errorCount>0?this._badgeVisible=!0:this._badgeVisible=!1}_onPanelDismissed(){this._panelDismissed=!0,this._errorCount>0&&(this._badgeVisible=!0)}_onRefreshing(){console.debug("bino-toolbar: refreshing \u2192 _refreshing=true"),this._refreshing=!0,this._refreshError=""}_onRefreshDone(){console.debug("bino-toolbar: refresh-done \u2192 _refreshing=false"),this._refreshing=!1}_onRefreshError(e){var t=e&&e.detail&&e.detail.message||"Refresh failed";console.debug("bino-toolbar: refresh-error",t),this._refreshError=String(t),this._refreshing=!1}};customElements.define("bino-toolbar",ze);var Le=class extends z{static properties={_errors:{state:!0},_visible:{state:!0}};static styles=L`
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
  `;constructor(){super(),this._errors=[],this._visible=!1,this._scanTimer=null,this._observer=null,this._badges=[],this._highlightTimer=null,this._boundOnShowErrors=this._onShowErrors.bind(this),this._boundOnContentUpdated=this._debouncedScan.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-show-errors",this._boundOnShowErrors),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._startObserver(),this._debouncedScan()}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-show-errors",this._boundOnShowErrors),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._observer&&(this._observer.disconnect(),this._observer=null),this._removeBadges()}updated(e){e.has("_visible")&&(this._visible?this.setAttribute("visible",""):this.removeAttribute("visible"))}render(){if(!this._visible||this._errors.length===0)return c``;var e=this,t=this._errors.length;return c`
      <div class="header">
        <span>${t} warning${t!==1?"s":""} found</span>
        <button class="close-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <ul>
        ${this._errors.map(function(r,n){return c`
            <li @click=${()=>e._scrollToElement(r.element)}>
              <span class="badge ${r.error.type||"warning"}">${r.error.type||"warning"}</span>
              <span class="message">${r.error.message||r.error.id||"Unknown error"}</span>
            </li>
          `})}
      </ul>
    `}_startObserver(){var e=this;this._observer=new MutationObserver(function(t){e._onMutation(t)}),this._observer.observe(document.body,{childList:!0,subtree:!0,attributes:!0,attributeFilter:["has-error","has-errors"]})}_onMutation(e){var t=!1;e.forEach(function(r){r.type==="attributes"&&(r.attributeName==="has-error"||r.attributeName==="has-errors")&&(t=!0),r.type==="childList"&&r.addedNodes.length>0&&r.addedNodes.forEach(function(n){n.nodeType===1&&n.hasAttribute&&(n.hasAttribute("has-error")||n.hasAttribute("has-errors"))&&(t=!0),n.nodeType===1&&n.querySelector&&n.querySelector("[has-error], [has-errors]")&&(t=!0)})}),t&&this._debouncedScan()}_debouncedScan(){var e=this;this._scanTimer&&clearTimeout(this._scanTimer),this._scanTimer=setTimeout(function(){e._scanForErrors()},100)}_parseErrors(e){if(!e)return[];try{var t=JSON.parse(e);return Array.isArray(t)?t:[]}catch{return[]}}_scanForErrors(){var e=[],t=document.querySelectorAll("[has-error], [has-errors]"),r=this;t.forEach(function(n){var i=n.getAttribute("has-error")||n.getAttribute("has-errors"),o=r._parseErrors(i);o.forEach(function(h){e.push({element:n,error:h})})}),this._errors=e,document.dispatchEvent(new CustomEvent("bino-errors-changed",{detail:{count:e.length}})),e.length>0?(this._visible=!0,this._injectBadges(e)):(this._visible=!1,this._removeBadges())}_highlightForElement(e){var t=this,r=this.renderRoot.querySelectorAll("li"),n=null;r.forEach(function(i,o){i.classList.remove("highlighted"),t._errors[o]&&t._errors[o].element===e&&(i.classList.add("highlighted"),n||(n=i))}),n&&n.scrollIntoView({block:"nearest",behavior:"smooth"}),this._highlightTimer&&clearTimeout(this._highlightTimer),this._highlightTimer=setTimeout(function(){r.forEach(function(i){i.classList.remove("highlighted")})},4e3)}_onClose(){this._visible=!1,document.dispatchEvent(new CustomEvent("bino-panel-dismissed"))}_onShowErrors(){this._errors.length>0&&(this._visible=!0)}_scrollToElement(e){e&&(e.scrollIntoView({behavior:"smooth",block:"center"}),e.classList.remove("bn-error-highlight"),e.offsetWidth,e.classList.add("bn-error-highlight"),setTimeout(function(){e.classList.remove("bn-error-highlight")},700))}_injectBadges(e){this._removeBadges();var t=this,r=new Map;e.forEach(function(n,i){r.has(n.element)||r.set(n.element,[]),r.get(n.element).push({error:n.error,index:i})}),r.forEach(function(n,i){var o=document.createElement("div");o.className="bn-error-indicator-badge",o.style.cssText="position:absolute;top:2px;right:2px;width:18px;height:18px;background:#fbc02d;color:#11161a;font-size:12px;border-radius:50%;display:flex;align-items:center;justify-content:center;z-index:10000;cursor:pointer;user-select:none;line-height:1;",o.textContent="\u26A0",o.title=n.map(function(p){return p.error.message||p.error.id||"Error"}).join(`
`),o.addEventListener("click",function(p){p.stopPropagation(),t._visible=!0,t._highlightForElement(i)});var h=i.parentNode;if(h){var u=window.getComputedStyle(h);u.position==="static"&&(h.style.position="relative"),i.insertAdjacentElement("afterend",o)}t._badges.push(o)})}_removeBadges(){this._badges.forEach(function(e){e.parentNode&&e.parentNode.removeChild(e)}),this._badges=[]}};customElements.define("bino-error-panel",Le);var Te=class extends z{static properties={_results:{state:!0},_activeIndex:{state:!0},_open:{state:!0}};static styles=L`
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
  `;constructor(){super(),this._results=[],this._activeIndex=-1,this._open=!1,this._debounceTimer=null,this._query=""}connectedCallback(){super.connectedCallback();var e=this;this._boundOutsideClick=function(t){!e.contains(t.target)&&!e.renderRoot.contains(t.target)&&e._close()},document.addEventListener("click",this._boundOutsideClick),this._boundContentUpdated=function(){e._query&&e._search(e._query)},document.addEventListener("bn-preview:content-updated",this._boundContentUpdated)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("click",this._boundOutsideClick),document.removeEventListener("bn-preview:content-updated",this._boundContentUpdated)}render(){var e=this,t=ie().startsWith("/doc/")?"Search headings...":"Search elements...";return c`
      <div class="search-wrap">
        <span class="search-icon">\u2315</span>
        <input type="text" placeholder=${t} autocomplete="off" spellcheck="false"
          @input=${this._onInput}
          @keydown=${this._onKeydown}
          @focus=${this._onFocus}>
      </div>
      <div class="dropdown ${this._open?"open":""}">
        ${this._results.length===0&&this._open?c`<div class="no-results">No results found</div>`:this._results.map(function(r,n){return c`
                <div class="result ${n===e._activeIndex?"active":""}"
                  @click=${()=>e._selectResult(n)}>
                  <span class="result-kind">${r.kind}</span>
                  <span class="result-name">${e._highlightMatch(r,e._query)}</span>
                </div>
              `})}
      </div>
    `}_highlightMatch(e,t){if(!t)return e.name;var r=t.toLowerCase(),n=e.name.toLowerCase().indexOf(r);if(n===-1)return e.name;var i=e.name.substring(0,n),o=e.name.substring(n,n+t.length),h=e.name.substring(n+t.length);return c`${i}<mark>${o}</mark>${h}`}_onInput(e){var t=this,r=e.target.value.trim();clearTimeout(this._debounceTimer),this._debounceTimer=setTimeout(function(){t._query=r,t._search(r)},150)}_onKeydown(e){if(e.key==="Escape"){this._close(),e.target.blur();return}if(e.key==="ArrowDown"){e.preventDefault(),this._moveActive(1);return}if(e.key==="ArrowUp"){e.preventDefault(),this._moveActive(-1);return}if(e.key==="Enter"){e.preventDefault(),this._activeIndex>=0&&this._activeIndex<this._results.length?this._selectResult(this._activeIndex):this._results.length>0&&this._selectResult(0);return}}_onFocus(){this._query&&this._results.length>0&&(this._open=!0)}_search(e){if(this._activeIndex=-1,!e||e.length<2){this._results=[],this._close();return}var t=e.toLowerCase(),r=[],n=new Set,i=document.querySelectorAll("[data-bino-kind]");i.forEach(function(u){var p=u.getAttribute("data-bino-kind")||"",y=u.getAttribute("data-bino-name")||"",E=p+":"+y;n.has(E)||(p.toLowerCase().indexOf(t)!==-1||y.toLowerCase().indexOf(t)!==-1)&&(n.add(E),r.push({type:"element",kind:p,name:y,el:u}))});var o=document.querySelectorAll("bn-layout-page[data-bino-page]");o.forEach(function(u){var p=u.getAttribute("data-bino-page")||"",y="page:"+p;n.has(y)||p.toLowerCase().indexOf(t)!==-1&&(n.add(y),r.push({type:"page",kind:"LayoutPage",name:p,el:u}))});var h=document.querySelectorAll(".bn-document-content h1, .bn-document-content h2, .bn-document-content h3, .bn-document-content h4, .bn-document-content h5, .bn-document-content h6");h.forEach(function(u){var p=(u.textContent||"").trim();if(p){var y="heading:"+(u.id||p);n.has(y)||p.toLowerCase().indexOf(t)!==-1&&(n.add(y),r.push({type:"heading",kind:u.tagName.toLowerCase()+" heading",name:p,el:u}))}}),r.length<50&&o.forEach(function(u){for(var p=u.getAttribute("data-bino-page")||"",y=u.shadowRoot||u,E=y.querySelectorAll("*"),v=0;v<E.length&&r.length<50;v++){var $=E[v];if(!($.tagName==="SCRIPT"||$.tagName==="STYLE"))for(var x=$.childNodes,S=0;S<x.length;S++){var C=x[S];if(C.nodeType===3){var g=C.textContent.trim();if(!(!g||g.length<2)){var d=g.toLowerCase().indexOf(t);if(d!==-1){var m=g.substring(Math.max(0,d-30),Math.min(g.length,d+e.length+30)),a="text:"+p+":"+g.substring(d,d+Math.min(40,g.length-d));if(n.has(a))continue;n.add(a),r.push({type:"text",kind:"text in "+p,name:m,el:$,query:e});break}}}}}}),this._results=r,this._open=!0}_moveActive(e){if(this._results.length!==0){var t=this._activeIndex+e;t<0&&(t=this._results.length-1),t>=this._results.length&&(t=0),this._activeIndex=t,this.updateComplete.then(()=>{var r=this.renderRoot.querySelectorAll(".result");r[t]&&r[t].scrollIntoView({block:"nearest"})})}}_selectResult(e){var t=this._results[e];if(!(!t||!t.el)){this._close(),t.el.scrollIntoView({behavior:"smooth",block:"center"});var r=t.el,n=r.style.outline,i=r.style.outlineOffset;r.style.outline="2px solid "+(getComputedStyle(document.documentElement).getPropertyValue("--bino-primary").trim()||"#0b727e"),r.style.outlineOffset="2px",setTimeout(function(){r.style.outline=n,r.style.outlineOffset=i},3e3)}}_close(){this._open=!1,this._activeIndex=-1}};customElements.define("bino-search",Te);var Me=class extends z{static properties={_documents:{state:!0},_open:{state:!0},_selectedDoc:{state:!0},_filterKind:{state:!0}};static styles=L`
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
  `;constructor(){super(),this._documents=[],this._open=!1,this._selectedDoc=null,this._filterKind="",this._boundOnOpen=this._onOpen.bind(this),this._boundOnChanged=this._onChanged.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-assets",this._boundOnOpen),document.addEventListener("bino-documents-changed",this._boundOnChanged),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-assets",this._boundOnOpen),document.removeEventListener("bino-documents-changed",this._boundOnChanged),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return k;var e=this,t=this._filteredDocs(),r=this._uniqueKinds();return c`
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
            ${r.map(function(n){var i=e._countKind(n);return c`
                <button class="kind-tab ${e._filterKind===n?"active":""}"
                  @click=${function(){e._filterKind=n,e._selectedDoc=null}}>
                  ${n} (${i})
                </button>
              `})}
          </div>
          <div class="doc-list">
            ${t.length===0?c`<div class="empty">No documents found</div>`:t.map(function(n){var i=e._selectedDoc===n;return c`
                    <div class="doc-row ${i?"selected":""}"
                      @click=${function(){e._selectedDoc=i?null:n}}>
                      <span class="kind-badge">${n.kind}</span>
                      <div class="doc-info">
                        <div class="doc-name">${n.name}</div>
                        <div class="doc-file">${n.file}</div>
                      </div>
                    </div>
                  `})}
          </div>
          ${this._selectedDoc?this._renderDetail(this._selectedDoc):k}
        </div>
      </div>
    `}_renderDetail(e){var t=e.labels||{},r=Object.keys(t),n=e.constraints||[];return c`
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
        ${r.length>0?c`
          <div class="detail-row">
            <span class="detail-label">Labels</span>
            <div class="pills">
              ${r.map(function(i){return c`<span class="pill label-pill">${i}: ${t[i]}</span>`})}
            </div>
          </div>
        `:k}
        ${n.length>0?c`
          <div class="detail-row">
            <span class="detail-label">Constraints</span>
            <div class="pills">
              ${n.map(function(i){return c`<span class="pill constraint-pill">${i}</span>`})}
            </div>
          </div>
        `:k}
      </div>
    `}_filteredDocs(){if(!this._filterKind)return this._documents;var e=this._filterKind;return this._documents.filter(function(t){return t.kind===e})}_uniqueKinds(){var e={},t=[];return this._documents.forEach(function(r){e[r.kind]||(e[r.kind]=!0,t.push(r.kind))}),t}_countKind(e){var t=0;return this._documents.forEach(function(r){r.kind===e&&t++}),t}_onOpen(e){this._documents=e.detail&&e.detail.documents||[],this._open=!0,this._selectedDoc=null,this._filterKind=""}_onChanged(e){if(this._open&&(this._documents=e.detail&&e.detail.documents||[],this._selectedDoc)){var t=this._selectedDoc,r=this._documents.some(function(n){return n.kind===t.kind&&n.name===t.name&&n.file===t.file});r||(this._selectedDoc=null)}}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_onBackdropClick(){this._close()}_stopPropagation(e){e.stopPropagation()}_close(){this._open=!1,this._selectedDoc=null}};customElements.define("bino-assets-modal",Me);var B=170,te=38,Re=20,Ut=56,ce=30,Bt={ReportArtefact:{bg:"#d4f7f9",stroke:"#0b727e",text:"#0c454c"},DocumentArtefact:{bg:"#d4f7f9",stroke:"#0b727e",text:"#0c454c"},LayoutPage:{bg:"#d6ecdd",stroke:"#1f7a3d",text:"#1f7a3d"},LayoutCard:{bg:"#ecfcfd",stroke:"#5cdae5",text:"#0c5a64"},Component:{bg:"#f7aace",stroke:"#e23e8c",text:"#c11f6e"},DataSet:{bg:"#fff0a8",stroke:"#d99e0b",text:"#1f262a"},DataSource:{bg:"#f6ddda",stroke:"#c0392b",text:"#c0392b"},MarkdownFile:{bg:"#eef2f3",stroke:"#9aa7ad",text:"#333c41"}},Kt={ReportArtefact:"Artefact",DocumentArtefact:"DocArtefact",LayoutPage:"Page",LayoutCard:"Card",Component:"Component",DataSet:"DataSet",DataSource:"Source",MarkdownFile:"Markdown"};function vt(s){return Bt[s]||{bg:"#eef2f3",stroke:"#9aa7ad",text:"#333c41"}}function jt(s){return Kt[s]||s}function Vt(s,e){return e||(e=20),!s||s.length<=e?s||"":s.substring(0,e-1)+"\u2026"}function Ft(s){if(!s||!s.rootId)return null;var e=s.nodes||{},t={},r={};function n(i){if(!i)return null;var o=e[i];if(!o)return null;if(t[i])return{id:i,kind:o.kind,name:o.name||i,children:[],cycle:!0};if(r[i])return{id:i,kind:o.kind,name:o.name||i,children:[],ref:!0};t[i]=!0,r[i]=!0;for(var h=[],u=o.dependsOn||[],p=0;p<u.length;p++){var y=n(u[p]);y&&h.push(y)}return t[i]=!1,{id:i,kind:o.kind,name:o.name||i,children:h}}return n(s.rootId)}function mt(s){if(!s)return 0;if(s.children.length===0)return s.w=B,B;for(var e=0,t=0;t<s.children.length;t++)e+=mt(s.children[t]);return e+=(s.children.length-1)*Re,s.w=Math.max(e,B),s.w}function Wt(s){if(!s)return{nodes:[],edges:[],width:0,height:0};mt(s);var e=[],t=[],r=0,n=0;function i(o,h,u){if(o){var p=h-B/2;if(e.push({id:o.id,kind:o.kind,name:o.name,x:p,y:u,cx:h,ref:o.ref||!1,cycle:o.cycle||!1}),h+B/2>r&&(r=h+B/2),u+te>n&&(n=u+te),o.children.length!==0){for(var y=u+te+Ut,E=0,v=0;v<o.children.length;v++)E+=o.children[v].w;E+=(o.children.length-1)*Re;for(var $=h-E/2,v=0;v<o.children.length;v++){var x=o.children[v],S=$+x.w/2;t.push({x1:h,y1:u+te,x2:S,y2:y}),i(x,S,y),$+=x.w+Re}}}}return i(s,s.w/2+ce,ce),{nodes:e,edges:t,width:r+ce,height:n+ce}}var De=class extends z{static properties={_graphData:{state:!0},_open:{state:!0}};static styles=L`
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
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;constructor(){super(),this._graphData=null,this._open=!1,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-graph",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-graph",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return k;var e=Ft(this._graphData),t=Wt(e);return c`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>Dependency Graph</h2>
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          <div class='graph-container'>
            ${this._renderSVG(t)}
          </div>
          ${this._renderLegend(t)}
        </div>
      </div>
    `}_renderSVG(e){if(!e||e.nodes.length===0)return c`<div class='empty'>No graph data available</div>`;var t=e.width,r=e.height,n=e.edges.map(function(o){var h=(o.y1+o.y2)/2;return Ae`<path d=${"M"+o.x1+","+o.y1+" C"+o.x1+","+h+" "+o.x2+","+h+" "+o.x2+","+o.y2}
        fill='none' stroke='#cbd4d8' stroke-width='1.5'/>`}),i=e.nodes.map(function(o){var h=vt(o.kind),u=o.ref||o.cycle?"0.5":"1",p=jt(o.kind),y=Vt(o.name),E=o.cycle?" [cycle]":o.ref?" [ref]":"";return Ae`
        <g opacity=${u}>
          <rect x=${o.x} y=${o.y} width=${B} height=${te}
            rx='6' fill=${h.bg} stroke=${h.stroke} stroke-width='1.5'/>
          <text x=${o.cx} y=${o.y+14} text-anchor='middle'
            font-size='10' font-weight='600' fill=${h.stroke}>${p}</text>
          <text x=${o.cx} y=${o.y+28} text-anchor='middle'
            font-size='11' fill=${h.text}>${y}${E}</text>
        </g>
      `});return c`
      <svg width=${t} height=${r} viewBox=${"0 0 "+t+" "+r}>
        ${n}
        ${i}
      </svg>
    `}_renderLegend(e){if(!e||e.nodes.length===0)return k;var t={},r=[];return e.nodes.forEach(function(n){t[n.kind]||(t[n.kind]=!0,r.push(n.kind))}),r.length===0?k:c`
      <div class='legend'>
        ${r.map(function(n){var i=vt(n);return c`
            <span class='legend-item'>
              <span class='legend-swatch' style=${"background:"+i.bg+";border:1.5px solid "+i.stroke}></span>
              ${n}
            </span>
          `})}
      </div>
    `}_onOpen(e){this._graphData=e.detail&&e.detail.graph||null,this._open=!0}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_close(){this._open=!1}_stop(e){e.stopPropagation()}};customElements.define("bino-graph-modal",De);var Qt=[25,50,100,250],gt=20,_t="bino-explorer-layout",he="bino-explorer-sql",Ne="bino-explorer-history",ue=180,pe=560,re=64,Jt=140;function K(s,e,t){return Math.min(Math.max(s,e),t)}function ne(s,e){try{var t=window.localStorage.getItem(s);return t!=null?JSON.parse(t):e}catch{return e}}function be(s,e){try{window.localStorage.setItem(s,JSON.stringify(e))}catch{}}var He=class extends z{static properties={_open:{state:!0},_metadata:{state:!0},_sql:{state:!0},_result:{state:!0},_summarizeResult:{state:!0},_loading:{state:!0},_error:{state:!0},_page:{state:!0},_pageSize:{state:!0},_activeTab:{state:!0},_expandedSource:{state:!0},_refreshing:{state:!0},_sidebarWidth:{state:!0},_editorHeight:{state:!0},_dragging:{state:!0},_history:{state:!0},_historyOpen:{state:!0},_exporting:{state:!0}};static styles=L`
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
  `;constructor(){super(),this._open=!1,this._metadata=null,this._sql=typeof ne(he,"")=="string"?ne(he,""):"",this._result=null,this._summarizeResult=null,this._loading=!1,this._error="",this._page=0,this._pageSize=50,this._activeTab="results",this._expandedSource=null,this._refreshing=!1;var e=ne(_t,{});this._sidebarWidth=K(Number(e.sidebarWidth)||280,ue,pe),this._editorHeight=Math.max(Number(e.editorHeight)||160,re),this._dragging=!1,this._history=Array.isArray(ne(Ne,[]))?ne(Ne,[]):[],this._historyOpen=!1,this._exporting=!1,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this),this._boundOnDocsChanged=this._onDocsChanged.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-explorer",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown),document.addEventListener("bino-documents-changed",this._boundOnDocsChanged)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-explorer",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown),document.removeEventListener("bino-documents-changed",this._boundOnDocsChanged)}render(){return this._open?c`
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
    `:k}_renderSidebar(){var e=this,t=this._metadata,r="width: "+this._sidebarWidth+"px; min-width: "+this._sidebarWidth+"px;";return t?c`
      <div class="sidebar" style=${r}>
        ${t.sources&&t.sources.length>0?c`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSources (${t.sources.length})</div>
            ${t.sources.map(function(n){var i=e._expandedSource===n.name;return c`
                <div>
                  <div class="sidebar-item" @click=${function(){e._selectSource(n.name)}}>
                    <span class="sidebar-item-name">${n.name}</span>
                    ${n.type?c`<span class="sidebar-item-badge badge-source">${n.type}</span>`:k}
                    <button class="sidebar-info-btn" title="Show columns"
                      @click=${function(o){o.stopPropagation(),e._toggleSourceInfo(n.name)}}>
                      ${i?"\u25B4":"\u25BE"}
                    </button>
                  </div>
                  ${i&&n.columns&&n.columns.length>0?c`
                    <div class="column-list">
                      ${n.columns.map(function(o){return c`
                          <div class="column-entry">
                            <span class="column-name">${o.name}</span>
                            <span class="column-type">${o.type}</span>
                          </div>
                        `})}
                    </div>
                  `:k}
                </div>
              `})}
          </div>
        `:k}
        ${t.datasets&&t.datasets.length>0?c`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSets (${t.datasets.length})</div>
            ${t.datasets.map(function(n){return c`
                <div class="sidebar-item" @click=${function(){e._selectDataset(n)}}
                     title=${n.sqlError?"cannot resolve dataset SQL: "+n.sqlError:n.sql||""}>
                  <span class="sidebar-item-name">${n.name}</span>
                  <span class="sidebar-item-badge badge-dataset">set</span>
                </div>
              `})}
          </div>
        `:k}
        ${(!t.sources||t.sources.length===0)&&(!t.datasets||t.datasets.length===0)?c`<div class="empty-state">No data sources found</div>`:k}
      </div>
    `:c`<div class="sidebar" style=${r}><div class="loading-msg">Loading...</div></div>`}_renderMainPanel(){var e=this;return c`
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
              ${this._historyOpen?this._renderHistoryMenu():k}
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
    `}_renderHistoryMenu(){var e=this;return this._history.length?c`
      <div class="history-menu" @click=${function(t){t.stopPropagation()}}>
        ${this._history.map(function(t){return c`
            <button class="history-item" title=${t.sql}
              @click=${function(){e._applyHistoryEntry(t)}}>
              <span class="history-sql">${t.sql}</span>
              <span class="history-time">${e._formatHistoryTime(t.ts)}</span>
            </button>
          `})}
      </div>
    `:c`<div class="history-menu"><div class="history-empty">No queries yet</div></div>`}_renderResults(){if(this._loading)return c`<div class="loading-msg">Executing query...</div>`;if(this._error)return c`<div class="error-msg">${this._error}</div>`;if(!this._result)return c`<div class="empty-state">Run a query to see results</div>`;if(this._result.error)return c`
        <div class="error-msg">${this._result.error}</div>
        ${this._result.durationMs!=null?c`<div class="status-bar"><span class="duration">${this._result.durationMs}ms</span></div>`:k}
      `;var e=this,t=this._result.columns||[],r=this._result.rows||[],n=this._result.totalRows,i=typeof n=="number"?Math.ceil(n/this._pageSize):null,o=this._page*this._pageSize;return c`
      <div class="table-container">
        ${t.length===0?c`<div class="empty-state">No columns returned</div>`:c`
            <table>
              <thead>
                <tr>
                  <th class="row-num">#</th>
                  ${t.map(function(h){return c`<th>${h.name}<span class="col-type">${h.type}</span></th>`})}
                </tr>
              </thead>
              <tbody>
                ${r.map(function(h,u){return c`<tr>
                    <td class="row-num">${o+u+1}</td>
                    ${h.map(function(p){return c`<td title="${p!=null?String(p):""}">${p!=null?String(p):""}</td>`})}
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
        <select .value=${String(this._pageSize)} @change=${function(h){e._pageSize=parseInt(h.target.value,10),e._page=0,e._rerunQuery()}}>
          ${Qt.map(function(h){return c`<option value=${h} ?selected=${h===e._pageSize}>${h} rows</option>`})}
        </select>
        <span class="duration">${this._result.durationMs!=null?this._result.durationMs+"ms":""}</span>
        ${typeof n=="number"?c`<span>${n} total rows</span>`:k}
      </div>
    `}_renderSummarizeTab(){if(this._loading)return c`<div class="loading-msg">Running SUMMARIZE...</div>`;if(!this._summarizeResult)return c`<div class="empty-state">Click "Summarize" to see column statistics</div>`;if(this._summarizeResult.error)return c`<div class="error-msg">${this._summarizeResult.error}</div>`;var e=this._summarizeResult.columns||[],t=this._summarizeResult.rows||[];return c`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              ${e.map(function(r){return c`<th>${r.name}<span class="col-type">${r.type}</span></th>`})}
            </tr>
          </thead>
          <tbody>
            ${t.map(function(r){return c`<tr>${r.map(function(n){return c`<td title="${n!=null?String(n):""}">${n!=null?String(n):""}</td>`})}</tr>`})}
          </tbody>
        </table>
      </div>
      <div class="status-bar">
        <span class="duration">${this._summarizeResult.durationMs!=null?this._summarizeResult.durationMs+"ms":""}</span>
      </div>
    `}_onOpen(){this._open=!0,this._fetchMetadata()}_close(){this._open=!1,this._historyOpen=!1}_onKeydown(e){if(this._open&&e.key==="Escape"){if(this._historyOpen){this._historyOpen=!1;return}this._close()}}_onExplorerClick(){this._historyOpen&&(this._historyOpen=!1)}_onDocsChanged(){this._open&&this._fetchMetadata()}_onRefresh(){if(!this._refreshing){this._refreshing=!0;var e=this;this._fetchMetadata(function(){e._refreshing=!1})}}_onSqlInput(e){this._sql=e.target.value,be(he,this._sql)}_setSql(e){this._sql=e,be(he,e)}_maxEditorHeight(){var e=this.renderRoot.querySelector(".main-panel");return e?Math.max(e.clientHeight-Jt,re):600}_startEditorDrag(e){e.preventDefault();var t=this,r=e.clientY,n=this._editorHeight,i=this._maxEditorHeight();this._dragging="editor";function o(u){t._editorHeight=K(n+(u.clientY-r),re,i)}function h(){window.removeEventListener("pointermove",o),window.removeEventListener("pointerup",h),t._dragging=!1,t._saveLayout()}window.addEventListener("pointermove",o),window.addEventListener("pointerup",h)}_startSidebarDrag(e){e.preventDefault();var t=this,r=e.clientX,n=this._sidebarWidth;this._dragging="sidebar";function i(h){t._sidebarWidth=K(n+(h.clientX-r),ue,pe)}function o(){window.removeEventListener("pointermove",i),window.removeEventListener("pointerup",o),t._dragging=!1,t._saveLayout()}window.addEventListener("pointermove",i),window.addEventListener("pointerup",o)}_onEditorSplitterKey(e){var t=16;e.key==="ArrowUp"?(e.preventDefault(),this._editorHeight=K(this._editorHeight-t,re,this._maxEditorHeight()),this._saveLayout()):e.key==="ArrowDown"&&(e.preventDefault(),this._editorHeight=K(this._editorHeight+t,re,this._maxEditorHeight()),this._saveLayout())}_onSidebarSplitterKey(e){var t=16;e.key==="ArrowLeft"?(e.preventDefault(),this._sidebarWidth=K(this._sidebarWidth-t,ue,pe),this._saveLayout()):e.key==="ArrowRight"&&(e.preventDefault(),this._sidebarWidth=K(this._sidebarWidth+t,ue,pe),this._saveLayout())}_saveLayout(){be(_t,{sidebarWidth:this._sidebarWidth,editorHeight:this._editorHeight})}_recordHistory(e){var t={sql:e,ts:Date.now()},r=this._history.filter(function(n){return n.sql!==e});r.unshift(t),r.length>gt&&(r.length=gt),this._history=r,be(Ne,r)}_applyHistoryEntry(e){this._setSql(e.sql),this._historyOpen=!1,this._page=0,this._runQuery()}_formatHistoryTime(e){if(!e)return"";var t=new Date(e),r=new Date,n=t.toDateString()===r.toDateString();return n?t.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"}):t.toLocaleDateString([],{month:"short",day:"numeric"})}_onEditorKeydown(e){if(e.key==="Tab"){e.preventDefault();var t=e.target,r=t.selectionStart,n=t.selectionEnd;this._setSql(this._sql.substring(0,r)+"  "+this._sql.substring(n)),this.updateComplete.then(function(){t.selectionStart=t.selectionEnd=r+2});return}e.key==="Enter"&&(e.metaKey||e.ctrlKey)&&(e.preventDefault(),this._runQuery())}_selectSource(e){this._setSql('SELECT * FROM "'+e+'"'),this._page=0,this._activeTab="results",this._runQuery()}_selectDataset(e){e&&e.sql?this._setSql(e.sql):e&&e.sqlError?this._setSql('-- Cannot resolve SQL for dataset "'+e.name+`":
-- `+e.sqlError):this._setSql('-- Dataset "'+(e&&e.name)+'" has no resolvable SQL.'),this._page=0,this._activeTab="results",e&&e.sql&&this._runQuery()}_toggleSourceInfo(e){this._expandedSource=this._expandedSource===e?null:e}_clearEditor(){this._setSql(""),this._result=null,this._summarizeResult=null,this._error="",this._page=0}_rerunQuery(){this._sql.trim()&&this._runQuery()}_runQuery(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="results",this._recordHistory(e),fetch(T()+"/__explorer/query",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e,limit:this._pageSize,offset:this._page*this._pageSize})}).then(function(r){return r.json()}).then(function(r){t._result=r,t._loading=!1}).catch(function(r){t._error=r.message||"Query failed",t._loading=!1})}}_runSummarize(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="summarize",fetch(T()+"/__explorer/summarize",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e})}).then(function(r){return r.json()}).then(function(r){t._summarizeResult=r,t._loading=!1}).catch(function(r){t._error=r.message||"Summarize failed",t._loading=!1})}}_exportCSV(){var e=this._sql.trim();if(!(!e||this._exporting)){var t=this;this._exporting=!0,fetch(T()+"/__explorer/export",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e})}).then(function(r){var n=r.headers.get("Content-Type")||"";return n.indexOf("text/csv")===-1?r.json().then(function(i){throw new Error(i.error||"Export failed")}):r.blob()}).then(function(r){var n=URL.createObjectURL(r),i=document.createElement("a");i.href=n,i.download="bino-explorer-export.csv",i.click(),URL.revokeObjectURL(n),t._exporting=!1}).catch(function(r){t._error=r.message||"Export failed",t._activeTab="results",t._exporting=!1})}}_fetchMetadata(e){var t=this;fetch(T()+"/__explorer/metadata").then(function(r){return r.json()}).then(function(r){t._metadata=r,e&&e()}).catch(function(r){console.error("explorer: fetch metadata failed",r),e&&e()})}};customElements.define("bino-data-explorer",He);var Yt=3,Ie=class extends z{static properties={_open:{state:!0},_supported:{state:!0},_state:{state:!0},_findings:{state:!0},_elements:{state:!0},_selectedId:{state:!0},_detail:{state:!0},_showBoxes:{state:!0},_provisional:{state:!0},_busy:{state:!0},_error:{state:!0}};static styles=L`
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
  `;constructor(){super(),this._open=!1,this._supported=!0,this._state=null,this._findings=[],this._elements=new Map,this._selectedId="",this._detail=null,this._showBoxes=!1,this._provisional=!1,this._retries=0,this._busy=!1,this._error="",this._captureTimer=null,this._overlays=[],this._boundOnOpen=this._onOpen.bind(this),this._boundOnContentUpdated=this._onContentUpdated.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-inspector",this._boundOnOpen),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-inspector",this._boundOnOpen),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated),document.removeEventListener("keydown",this._boundOnKeydown),this._captureTimer&&clearTimeout(this._captureTimer),this._clearOverlays()}updated(e){e.has("_open")&&(this._open?this.setAttribute("open",""):this.removeAttribute("open"))}render(){return c`
      <div class="header">
        <span>Inspector</span>
        <span class="spacer"></span>
        ${this._supported?c`
              <button class="icon-btn" title="Outline every component"
                aria-pressed=${this._showBoxes?"true":"false"} @click=${this._onToggleBoxes}>□</button>
              <button class="icon-btn" title="Copy the snapshot as JSON" @click=${this._onCopy}>⎘</button>
              <button class="icon-btn" title="Re-capture" @click=${this._onRefresh}>↻</button>
            `:k}
        <button class="icon-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <div class="body">${this._renderBody()}</div>
    `}_renderBody(){if(!this._supported)return c`
        <p class="notice">
          This template engine does not expose layout state. The inspector needs
          <code>bn-template-engine v1.0.0-next.24</code> or newer — pin it with
          <code>engine-version</code> in <code>bino.toml</code>.
        </p>
      `;if(this._error)return c`<p class="notice">${this._error}</p>`;if(this._busy&&!this._state)return c`<p class="notice">Capturing…</p>`;if(!this._state)return c`<p class="notice">No snapshot yet.</p>`;var e=this._state.components||[];return c`
      ${this._provisional?c`
            <p class="notice provisional">
              The report is still rendering, so these boxes are provisional and
              no checks were run. Re-capturing…
            </p>
          `:k}
      ${this._findings.length>0?c`
            <div class="section-title">${this._findings.length} finding${this._findings.length===1?"":"s"}</div>
            <ul>${this._findings.map(t=>this._renderFinding(t))}</ul>
          `:k}
      <div class="section-title">${e.length} component${e.length===1?"":"s"}</div>
      <ul>${e.map(t=>this._renderComponent(t))}</ul>
    `}_renderFinding(e){var t=e.name?e.kind?e.kind+" "+e.name:e.name:e.componentId;return c`
      <li class="finding ${e.severity==="error"?"error":""}"
        @click=${()=>this._select(e.componentId)}>
        <span class="finding-label">${t}</span> — ${e.message}
        ${e.hint?c`<span class="finding-hint">${e.hint}</span>`:k}
      </li>
    `}_renderComponent(e){var t=this._state.sources&&this._state.sources[e.id]||{},r=t.name||t.ref||e.id,n=e.id===this._selectedId;return c`
      <li>
        <div class="row ${n?"selected":""}"
          @click=${i=>this._onRowClick(i,e.id)}
          @mouseenter=${()=>this._outline(e.id)}
          @mouseleave=${()=>this._clearHoverOutline()}>
          <div class="row-label">
            <span>${t.kind?t.kind+" "+r:r}</span>
            <span class="row-tag">${e.tag}</span>
          </div>
          <div class="chips">
            <span class="chip">${this._formatBox(e)}</span>
            ${this._countChips(e)}
            ${this._scaleChip(e)}
            ${this._diagChips(e)}
          </div>
        </div>
        ${n?this._renderDetail(e):k}
      </li>
    `}_renderDetail(e){var t=[],r=e.em||{};t.push(["font",this._round(r.fontSizePx)+"px"]),r.appliedScaleFactor!=null&&r.appliedScaleFactor!==1&&t.push(["auto-fit",Math.round(r.appliedScaleFactor*100)+"%"]);var n=e.scaling||{};n.unitsPerEm!=null&&t.push(["units/em",this._round(n.unitsPerEm)+" ("+(n.unitMode||"auto")+")"]),n.percentagePointsPerEm!=null&&t.push(["pp/em",this._round(n.percentagePointsPerEm)+" ("+(n.percentageMode||"auto")+")"]),(e.regions||[]).forEach(function(o){t.push([o.id,Math.round(o.rect.component.width)+"\xD7"+Math.round(o.rect.component.height)])});var i=e.metadata||{};return i.scenarios&&i.scenarios.length>0&&t.push(["scenarios",i.scenarios.join(", ")]),i.variances&&i.variances.length>0&&t.push(["variances",i.variances.join(", ")]),(e.diagnostics||[]).forEach(function(o){t.push([o.id||o.type||"diagnostic",o.message||""])}),c`
      <div class="detail">
        <dl>${t.map(o=>c`<dt>${o[0]}</dt><dd>${o[1]}</dd>`)}</dl>
        ${this._detail?this._renderElementDetail():k}
        <div class="detail-actions">
          ${this._detail?k:c`<button class="text-btn" @click=${()=>this._loadDetail(e.id)}>Element detail</button>`}
          <button class="text-btn" @click=${()=>this._revealSource(e.id)}>Reveal source</button>
        </div>
      </div>
    `}_renderElementDetail(){var e={};(this._detail.elements||[]).forEach(function(n){e[n.kind]=(e[n.kind]||0)+1});var t=Object.keys(e).sort(),r=this._detail.table&&this._detail.table.columns||[];return c`
      <dl>
        ${t.map(n=>c`<dt>${n}</dt><dd>${e[n]}</dd>`)}
        ${r.map(n=>c`<dt>col ${n.index}</dt><dd>${n.key}${n.bucket?" \xB7 "+n.bucket:""}</dd>`)}
      </dl>
    `}_formatBox(e){var t=e.rect&&e.rect.component||{width:0,height:0};return Math.round(t.width)+"\xD7"+Math.round(t.height)}_countChips(e){var t=e.metadata||{},r=[["bars",t.barCount],["points",t.pointCount],["rows",t.rowCount],["nodes",t.nodeCount]];return r.filter(function(n){return n[1]!=null}).map(function(n){return c`<span class="chip ${n[1]===0?"warn":""}">${n[1]} ${n[0]}</span>`})}_scaleChip(e){var t=e.scaling||{};return t.unitsPerEm==null?k:c`<span class="chip scale">${t.unitMode||"auto"} ${this._round(t.unitsPerEm)}/em</span>`}_diagChips(e){return(e.diagnostics||[]).map(function(t){return c`<span class="chip ${t.type==="error"?"bad":"warn"}">${t.id||t.type}</span>`})}_round(e){return e==null?"":Math.round(e*100)/100}_onOpen(){this._open=!0,this._retries=0,this._capture()}_onClose(){this._open=!1,this._showBoxes=!1,this._clearOverlays()}_onKeydown(e){e.key==="Escape"&&this._open&&this._onClose()}_onContentUpdated(){if(this._open){this._captureTimer&&clearTimeout(this._captureTimer);var e=this;this._captureTimer=setTimeout(function(){e._retries=0,e._capture()},250)}}_onRefresh(){this._retries=0,this._capture()}_scheduleRetry(){if(!(this._retries>=Yt)){this._retries++;var e=this;this._captureTimer&&clearTimeout(this._captureTimer),this._captureTimer=setTimeout(function(){e._capture()},1e3)}}async _capture(){if(this._supported=de(),!this._supported){this._state=null;return}this._busy=!0,this._error="";try{var e=await bt({settleTimeoutMs:5e3});if(!e){this._supported=!1;return}var t=this._selectedId;this._elements=e.elements,this._detail=null,this._state=Object.assign({},e.state,{sources:e.sources}),this._selectedId=e.elements.has(t)?t:"",this._provisional=!e.settled,this._provisional?(this._findings=[],this._scheduleRetry()):this._findings=await this._analyze(e),this._showBoxes&&this._drawAllOutlines()}catch(r){console.error("bino inspector: capture failed",r),this._error="Capture failed: "+(r&&r.message?r.message:r)}finally{this._busy=!1}}async _analyze(e){try{var t=await fetch(T()+"/__bino/layout-state",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({state:e.state,sources:e.sources})});if(!t.ok)return console.warn("bino inspector: analysis failed",t.status),[];var r=await t.json();return r.findings||[]}catch(n){return console.warn("bino inspector: analysis request failed",n),[]}}async _loadDetail(e){var t=this._elements.get(e);try{this._detail=await ft(t)||{elements:[]}}catch(r){console.warn("bino inspector: element detail failed",r),this._detail={elements:[]}}}_onRowClick(e,t){if(e.metaKey||e.ctrlKey){this._revealSource(t);return}this._select(t)}_select(e){if(this._selectedId===e){this._selectedId="",this._detail=null;return}this._selectedId=e,this._detail=null;var t=this._elements.get(e);t&&(t.scrollIntoView({behavior:"smooth",block:"center"}),this._outline(e,!0))}_revealSource(e){var t=this._elements.get(e),r=t&&t.closest?t.closest("[data-bino-kind]"):null;if(r){var n={type:"bino:revealSource",kind:r.getAttribute("data-bino-kind"),name:r.getAttribute("data-bino-name")||"",ref:r.getAttribute("data-bino-ref")||""};window.parent&&window.parent!==window&&window.parent.postMessage(n,"*")}}_outline(e,t){var r=this._elements.get(e);r&&(this._showBoxes||this._clearOverlays(),this._overlays.push(this._makeOutline(r,t?"selected":"hover")))}_clearHoverOutline(){this._showBoxes||this._clearOverlays()}_onToggleBoxes(){this._showBoxes=!this._showBoxes,this._clearOverlays(),this._showBoxes&&this._drawAllOutlines()}_drawAllOutlines(){var e=this;this._elements.forEach(function(t){e._overlays.push(e._makeOutline(t,"all"))})}_makeOutline(e,t){var r=e.getBoundingClientRect(),n=document.createElement("div");return n.className="bn-inspect-outline",n.style.cssText="position:absolute;pointer-events:none;left:"+(r.left+window.scrollX)+"px;top:"+(r.top+window.scrollY)+"px;width:"+r.width+"px;height:"+r.height+"px;",t==="selected"&&n.classList.add("selected"),document.body.appendChild(n),n}_clearOverlays(){this._overlays.forEach(function(e){e.parentNode&&e.parentNode.removeChild(e)}),this._overlays=[]}async _onCopy(){if(this._state){var e=JSON.stringify({state:this._state,findings:this._findings},null,2);try{await navigator.clipboard.writeText(e)}catch(t){console.warn("bino inspector: clipboard write failed",t)}}}};customElements.define("bino-inspector",Ie);if(!(!window.EventSource||window.__bnPreviewRuntime)){let s=function(){qe&&Ue&&e("initial")},e=function(r){var n=++Be;fetch(Pe+"/__preview/context?path="+encodeURIComponent(N)).then(function(i){return i.ok?i.text():(console.warn("bn preview: context fetch failed",i.status,r),null)}).then(function(i){if(!(!i||n!==Be)){var o=We(i,yt);if(o){Ke=!0;try{document.dispatchEvent(new CustomEvent("bn-preview:content-updated",{detail:{path:N}}))}catch(h){console.debug("bn preview: custom event skipped",h)}}else console.warn("bn preview: swapContext returned false; DOM not updated")}}).catch(function(i){console.error("bn preview: context fetch errored",i)})},t=function(){if(N==="/"){document.querySelectorAll(".bn-page-info").forEach(function(p){p.remove()});var r=document.querySelector("bn-context");if(r){var n=r.getAttribute("data-page-meta");if(n){var i;try{i=JSON.parse(n)}catch{return}if(Array.isArray(i)){var o={};i.forEach(function(p){o[p.name]=p});var h=r.shadowRoot||r,u=h.querySelectorAll("bn-layout-page[data-bino-page]");u.length===0&&(u=document.querySelectorAll("bn-layout-page[data-bino-page]")),u.forEach(function(p){var y=p.getAttribute("data-bino-page");if(y){var E=y.split("#")[0],v=o[E]||o[y];if(v){var $=document.createElement("div");$.className="bn-page-info";var x=document.createElement("span");if(x.className="bn-page-info-name",x.textContent=y,$.appendChild(x),v.constraints&&v.constraints.length>0){var S=document.createElement("span");S.className="bn-page-info-label",S.textContent="constraints:",$.appendChild(S),v.constraints.forEach(function(g){var d=document.createElement("span");d.className="bn-page-info-pill constraint",d.textContent=g,$.appendChild(d)})}if(v.artifacts&&v.artifacts.length>0){var C=document.createElement("span");C.className="bn-page-info-label",C.textContent="used in:",$.appendChild(C),v.artifacts.forEach(function(g){var d=document.createElement("span");d.className="bn-page-info-pill artefact",d.textContent=g,$.appendChild(d)})}p.parentNode.insertBefore($,p)}}})}}}}};window.__bnPreviewRuntime=!0,console.info("bn preview runtime v12 (doc routes live-reload)"),yt=new DOMParser,Pe=T(),N=ie(),j=new EventSource(Pe+"/__preview/events"),qe=!1,Ue=!1,Fe().then(function(){Ue=!0,s()}),j.addEventListener("ready",function(){qe=!0,document.dispatchEvent(new CustomEvent("bn-preview:refresh-done")),s()}),j.addEventListener("refreshing",function(r){try{var n=JSON.parse(r.data||"{}");document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:n}))}catch{document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:{}}))}}),j.addEventListener("refresh-done",function(r){var n={};try{n=JSON.parse(r.data||"{}")||{}}catch{n={}}var i=Array.isArray(n.paths)?n.paths:[],o=!1;if(i.length>0){for(var h=0;h<i.length;h++)if(W(i[h])===N){o=!0;break}o||(console.warn("bn preview: refresh did not include this view",N,"broadcast paths:",i),document.dispatchEvent(new CustomEvent("bn-preview:no-payload",{detail:{path:N,broadcastPaths:i}})))}o&&!Ke&&e("initial-retry"),document.dispatchEvent(new CustomEvent("bn-preview:refresh-done",{detail:n}))}),Be=0,Ke=!1,j.addEventListener("path-changed",function(r){var n={};try{n=JSON.parse(r.data||"{}")||{}}catch{return}!n.path||W(n.path)!==N||e("path-changed")}),j.addEventListener("refresh-error",function(r){var n={};try{n=JSON.parse(r.data||"{}")}catch{}n&&n.path&&W(n.path)!==N||(console.error("bn preview: refresh failed",n&&n.message),document.dispatchEvent(new CustomEvent("bn-preview:refresh-error",{detail:n})))}),window.addEventListener("beforeunload",function(){j.close()}),document.addEventListener("click",function(r){if(!(!r.metaKey&&!r.ctrlKey)){var n=r.target.closest("[data-bino-kind], [data-bino-file]");if(n){var i;n.hasAttribute("data-bino-kind")?i={type:"bino:revealSource",kind:n.getAttribute("data-bino-kind"),name:n.getAttribute("data-bino-name")||"",ref:n.getAttribute("data-bino-ref")||""}:i={type:"bino:revealSource",file:n.getAttribute("data-bino-file")},window.parent&&window.parent!==window&&window.parent.postMessage(i,"*"),r.preventDefault(),r.stopPropagation()}}}),document.addEventListener("bn-preview:content-updated",function(){t()}),document.readyState==="loading"?document.addEventListener("DOMContentLoaded",t):t()}var yt,Pe,N,j,qe,Ue,Be,Ke;
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
